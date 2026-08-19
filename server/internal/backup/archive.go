package backup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	InstanceArchiveFormat  = "argus-instance-backup"
	InstanceArchiveVersion = 1
	maxArchiveFileSize     = 512 << 20
	maxArchiveTotalSize    = 2 << 30
)

var ErrArchiveFormat = errors.New("invalid argus instance archive")

type ArchiveEntry struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Mode     uint32 `json:"mode"`
	Required bool   `json:"required"`
}

type InstanceManifest struct {
	Format           string         `json:"format"`
	Version          int            `json:"version"`
	CreatedAt        time.Time      `json:"created_at"`
	ArgusVersion     string         `json:"argus_version,omitempty"`
	CredentialPolicy string         `json:"credential_policy"`
	Entries          []ArchiveEntry `json:"entries"`
	Excluded         []string       `json:"excluded"`
	ManifestSHA256   string         `json:"manifest_sha256,omitempty"`
}

func canonicalManifest(m InstanceManifest) ([]byte, error) {
	m.ManifestSHA256 = ""
	return json.Marshal(m)
}

func (m *InstanceManifest) Finalize() error {
	if m.Format != InstanceArchiveFormat || m.Version != InstanceArchiveVersion {
		return ErrArchiveFormat
	}
	b, err := canonicalManifest(*m)
	if err != nil {
		return err
	}
	h := sha256.Sum256(b)
	m.ManifestSHA256 = hex.EncodeToString(h[:])
	return nil
}

func (m InstanceManifest) Validate() error {
	if m.Format != InstanceArchiveFormat || m.Version != InstanceArchiveVersion || len(m.Entries) == 0 {
		return ErrArchiveFormat
	}
	seen := make(map[string]struct{}, len(m.Entries))
	var total int64
	hasDB := false
	for _, e := range m.Entries {
		if e.Path == "db/argus.db" && e.Kind == "database" && e.Required {
			hasDB = true
		}
		if len(e.SHA256) != 64 {
			return fmt.Errorf("%w: invalid hash for %q", ErrArchiveFormat, e.Path)
		}
		if _, err := hex.DecodeString(e.SHA256); err != nil {
			return fmt.Errorf("%w: invalid hash for %q", ErrArchiveFormat, e.Path)
		}
		if !safeArchivePath(e.Path) || e.Size < 0 || e.Size > maxArchiveFileSize {
			return fmt.Errorf("%w: unsafe entry %q", ErrArchiveFormat, e.Path)
		}
		if _, ok := seen[e.Path]; ok {
			return fmt.Errorf("%w: duplicate entry %q", ErrArchiveFormat, e.Path)
		}
		seen[e.Path] = struct{}{}
		total += e.Size
		if total > maxArchiveTotalSize {
			return fmt.Errorf("%w: archive too large", ErrArchiveFormat)
		}
	}
	if !hasDB {
		return fmt.Errorf("%w: database entry missing", ErrArchiveFormat)
	}
	if m.ManifestSHA256 == "" {
		return fmt.Errorf("%w: manifest hash missing", ErrArchiveFormat)
	}
	b, err := canonicalManifest(m)
	if err != nil {
		return err
	}
	h := sha256.Sum256(b)
	if !strings.EqualFold(m.ManifestSHA256, hex.EncodeToString(h[:])) {
		return fmt.Errorf("%w: manifest hash mismatch", ErrArchiveFormat)
	}
	return nil
}

func safeArchivePath(p string) bool {
	if p == "" || filepath.IsAbs(p) || strings.Contains(p, "\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	return clean == p && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.HasPrefix(clean, "/")
}

// CreateInstanceArchive writes a versioned ZIP containing a consistent DB snapshot
// and only the explicitly supported instance asset roots.
func CreateInstanceArchive(output, dbSnapshot, root, scriptsRoot string) (InstanceManifest, error) {
	files := make(map[string]string)
	files["db/argus.db"] = dbSnapshot
	for _, spec := range []struct{ prefix, dir string }{{"themes", filepath.Join(root, "themes")}, {"plugins", filepath.Join(root, "plugins")}, {"scripts", scriptsRoot}} {
		if spec.dir == "" {
			continue
		}
		if err := collectArchiveFiles(files, spec.prefix, spec.dir); err != nil {
			return InstanceManifest{}, err
		}
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	m := InstanceManifest{Format: InstanceArchiveFormat, Version: InstanceArchiveVersion, CreatedAt: time.Now().UTC(), CredentialPolicy: "encrypted-in-db; external-secrets-excluded", Excluded: []string{"*.jwt", "ARGUS_BACKUP_KEY", "ARGUS_BACKUP_KEY_FILE", "*.wal", "*.shm", "staging/**"}}
	for _, p := range paths {
		st, err := os.Lstat(files[p])
		if err != nil {
			return InstanceManifest{}, err
		}
		if !st.Mode().IsRegular() {
			return InstanceManifest{}, fmt.Errorf("%w: non-regular file %q", ErrArchiveFormat, p)
		}
		h, size, err := hashFile(files[p])
		if err != nil {
			return InstanceManifest{}, err
		}
		m.Entries = append(m.Entries, ArchiveEntry{Path: p, Kind: kindForArchivePath(p), Size: size, SHA256: h, Mode: uint32(st.Mode().Perm()), Required: strings.HasPrefix(p, "db/")})
	}
	if err := m.Finalize(); err != nil {
		return InstanceManifest{}, err
	}
	if err := writeInstanceZip(output, m, files); err != nil {
		return InstanceManifest{}, err
	}
	return m, nil
}

func collectArchiveFiles(out map[string]string, prefix, dir string) error {
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink %q", ErrArchiveFormat, path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(prefix, rel))
		if !safeArchivePath(name) || strings.HasSuffix(name, ".tmp") || strings.Contains(name, "/.staging-") {
			return nil
		}
		if _, exists := out[name]; exists {
			return fmt.Errorf("%w: duplicate entry %q", ErrArchiveFormat, name)
		}
		out[name] = path
		return nil
	})
}

func kindForArchivePath(p string) string {
	if strings.HasPrefix(p, "db/") {
		return "database"
	}
	if strings.HasPrefix(p, "themes/") {
		return "theme"
	}
	if strings.HasPrefix(p, "plugins/") {
		return "plugin"
	}
	return "script"
}

func writeInstanceZip(output string, m InstanceManifest, files map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return err
	}
	tmp := output + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		f.Close()
		return err
	}
	mw, err := zw.Create("manifest.json")
	if err != nil {
		f.Close()
		return err
	}
	if _, err = mw.Write(manifest); err != nil {
		f.Close()
		return err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		w, err := zw.Create(p)
		if err != nil {
			f.Close()
			return err
		}
		src, err := os.Open(files[p])
		if err != nil {
			f.Close()
			return err
		}
		_, cpErr := io.Copy(w, src)
		closeErr := src.Close()
		if cpErr != nil {
			f.Close()
			return cpErr
		}
		if closeErr != nil {
			f.Close()
			return closeErr
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, output)
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// ExtractInstanceArchive validates and extracts an instance archive into a new
// directory. It never follows symlinks and refuses entries absent from the manifest.
func ExtractInstanceArchive(path, destination string) (InstanceManifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return InstanceManifest{}, fmt.Errorf("%w: %v", ErrArchiveFormat, err)
	}
	defer zr.Close()
	var manifest InstanceManifest
	contents := make(map[string][]byte)
	for _, f := range zr.File {
		if !safeArchivePath(f.Name) || f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return InstanceManifest{}, fmt.Errorf("%w: unsafe entry %q", ErrArchiveFormat, f.Name)
		}
		if _, exists := contents[f.Name]; exists {
			return InstanceManifest{}, fmt.Errorf("%w: duplicate path %q", ErrArchiveFormat, f.Name)
		}
		r, err := f.Open()
		if err != nil {
			return InstanceManifest{}, err
		}
		b, err := io.ReadAll(io.LimitReader(r, maxArchiveFileSize+1))
		_ = r.Close()
		if err != nil {
			return InstanceManifest{}, err
		}
		if int64(len(b)) > maxArchiveFileSize {
			return InstanceManifest{}, fmt.Errorf("%w: entry too large", ErrArchiveFormat)
		}
		contents[f.Name] = b
	}
	raw, ok := contents["manifest.json"]
	if !ok || json.Unmarshal(raw, &manifest) != nil {
		return InstanceManifest{}, ErrArchiveFormat
	}
	if err := manifest.Validate(); err != nil {
		return InstanceManifest{}, err
	}
	declared := make(map[string]ArchiveEntry, len(manifest.Entries))
	for _, e := range manifest.Entries {
		declared[e.Path] = e
	}
	if len(contents) != len(declared)+1 {
		return InstanceManifest{}, fmt.Errorf("%w: undeclared entries", ErrArchiveFormat)
	}
	for p, b := range contents {
		if p == "manifest.json" {
			continue
		}
		e, ok := declared[p]
		if !ok || int64(len(b)) != e.Size {
			return InstanceManifest{}, fmt.Errorf("%w: missing or invalid entry %q", ErrArchiveFormat, p)
		}
		h := sha256.Sum256(b)
		if !strings.EqualFold(e.SHA256, hex.EncodeToString(h[:])) {
			return InstanceManifest{}, fmt.Errorf("%w: hash mismatch %q", ErrArchiveFormat, p)
		}
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		return InstanceManifest{}, err
	}
	for p, b := range contents {
		if p == "manifest.json" {
			continue
		}
		dst := filepath.Join(destination, filepath.FromSlash(p))
		if !strings.HasPrefix(filepath.Clean(dst), filepath.Clean(destination)+string(os.PathSeparator)) {
			return InstanceManifest{}, fmt.Errorf("%w: destination escape", ErrArchiveFormat)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return InstanceManifest{}, err
		}
		if err := os.WriteFile(dst, b, 0600); err != nil {
			return InstanceManifest{}, err
		}
	}
	return manifest, nil
}

// InspectInstanceArchive validates manifest and every entry hash without extracting.
func InspectInstanceArchive(path string) (InstanceManifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return InstanceManifest{}, fmt.Errorf("%w: %v", ErrArchiveFormat, err)
	}
	defer zr.Close()
	var m InstanceManifest
	data := make(map[string][]byte)
	for _, f := range zr.File {
		if !safeArchivePath(f.Name) {
			return InstanceManifest{}, fmt.Errorf("%w: unsafe path %q", ErrArchiveFormat, f.Name)
		}
		if _, exists := data[f.Name]; exists {
			return InstanceManifest{}, fmt.Errorf("%w: duplicate path %q", ErrArchiveFormat, f.Name)
		}
		r, err := f.Open()
		if err != nil {
			return InstanceManifest{}, err
		}
		b, err := io.ReadAll(io.LimitReader(r, maxArchiveFileSize+1))
		r.Close()
		if err != nil {
			return InstanceManifest{}, err
		}
		if int64(len(b)) > maxArchiveFileSize {
			return InstanceManifest{}, fmt.Errorf("%w: entry too large", ErrArchiveFormat)
		}
		data[f.Name] = b
	}
	raw, ok := data["manifest.json"]
	if !ok || json.Unmarshal(raw, &m) != nil {
		return InstanceManifest{}, ErrArchiveFormat
	}
	if err := m.Validate(); err != nil {
		return InstanceManifest{}, err
	}
	for _, e := range m.Entries {
		b, ok := data[e.Path]
		if !ok || int64(len(b)) != e.Size {
			return InstanceManifest{}, fmt.Errorf("%w: missing entry %q", ErrArchiveFormat, e.Path)
		}
		h := sha256.Sum256(b)
		if !strings.EqualFold(e.SHA256, hex.EncodeToString(h[:])) {
			return InstanceManifest{}, fmt.Errorf("%w: hash mismatch %q", ErrArchiveFormat, e.Path)
		}
	}
	return m, nil
}
