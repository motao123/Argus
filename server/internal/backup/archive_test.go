package backup

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestInstanceArchiveRoundtripAndManifest(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "snapshot.db")
	if err := os.WriteFile(db, []byte("SQLite format 3\x00snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(root, "themes", "night", "theme.css")
	if err := os.MkdirAll(filepath.Dir(asset), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset, []byte("body{}"), 0600); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(root, "plugins", "demo", "plugin.js")
	if err := os.MkdirAll(filepath.Dir(plugin), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plugin, []byte("console.log('ok')"), 0600); err != nil {
		t.Fatal(err)
	}
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "notify-1.js"), []byte("send()"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "instance.arguszip")
	m, err := CreateInstanceArchive(archive, db, root, scripts)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := InspectInstanceArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 4 {
		t.Fatalf("entries=%d want 4", len(got.Entries))
	}
	destination := filepath.Join(root, "unpacked")
	if _, err := ExtractInstanceArchive(archive, destination); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"db/argus.db", "themes/night/theme.css", "plugins/demo/plugin.js", "scripts/notify-1.js"} {
		if _, err := os.Stat(filepath.Join(destination, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing extracted %s: %v", rel, err)
		}
	}
}

func TestInstanceArchiveRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "snapshot.db")
	if err := os.WriteFile(db, []byte("db"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "plugins", "bad"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(db, filepath.Join(root, "plugins", "bad", "link")); err != nil {
		t.Skip("symlink unavailable")
	}
	_, err := CreateInstanceArchive(filepath.Join(root, "out.zip"), db, root, "")
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestInspectInstanceArchiveRejectsTamperedEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write([]byte(`{"format":"argus-instance-backup","version":1,"credential_policy":"x","entries":[{"path":"db/argus.db","kind":"database","size":2,"sha256":"00","required":true}],"excluded":[],"manifest_sha256":"00"}`))
	w, _ = zw.Create("db/argus.db")
	_, _ = w.Write([]byte("db"))
	_ = zw.Close()
	_ = f.Close()
	if _, err := InspectInstanceArchive(path); err == nil {
		t.Fatal("expected manifest hash rejection")
	}
}
