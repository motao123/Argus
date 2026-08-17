// Package backup 实现定时加密备份：VACUUM INTO 一致性快照 → AES-256-GCM 加密 →
// HTTP PUT / 本地目录写入，附带保留策略与恢复演练（解密临时库 integrity_check）。
package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/hkdf"
)

// 加密备份文件格式（单文件，含头部）：
//
//	[0:8]    magic  "ARGUSENC"
//	[8]      version（当前 1）
//	[9:17]   keyID（派生密钥指纹前 8 字节，用于检测密钥轮换/不匹配）
//	[17:29]  nonce（12 字节随机）
//	[29:]    AES-256-GCM 密文（密文 + 16 字节 GCM tag）
var (
	encMagic     = [8]byte{'A', 'R', 'G', 'U', 'S', 'E', 'N', 'C'}
	encVersion   = byte(1)
	headerLen    = 8 + 1 + 8 + 12
	keyIDLen     = 8
	encInfoLabel = "argus-backup-aes-gcm-v1"
)

// ErrKeyMismatch 解密时密钥指纹不一致（密钥已轮换或选错计划）。
var ErrKeyMismatch = errors.New("encryption key mismatch (key rotated or wrong schedule)")

// ErrBadFormat 文件不是合法的 Argus 加密备份。
var ErrBadFormat = errors.New("not an argus encrypted backup")

// NewSalt 生成计划级随机盐（16 字节 hex），用于 HKDF 派生。
func NewSalt() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// DeriveKey 用 HKDF-SHA256 从密钥材料派生 32 字节 AES 密钥。
// 盐为每计划随机值（非机密，落库），info 固定域分隔，防跨计划/跨用途派生碰撞。
// 返回派生密钥与其 8 字节指纹（hex 16 字符）。
func DeriveKey(material []byte, saltHex, info string) ([]byte, string, error) {
	if len(material) == 0 {
		return nil, "", errors.New("empty key material")
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil || len(salt) == 0 {
		return nil, "", errors.New("invalid key salt")
	}
	if info == "" {
		info = encInfoLabel
	}
	r := hkdf.New(sha256.New, material, salt, []byte(info))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, "", fmt.Errorf("hkdf: %w", err)
	}
	sum := sha256.Sum256(key)
	return key, hex.EncodeToString(sum[:keyIDLen]), nil
}

// EncryptFile 读取 srcPath 明文，AES-256-GCM 加密写入 dstPath（原子替换），
// 返回密钥指纹、密文 SHA-256 与密文大小。
func EncryptFile(srcPath, dstPath string, key []byte) (keyID, sha256Hex string, size int64, err error) {
	plain, err := os.ReadFile(srcPath)
	if err != nil {
		return "", "", 0, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", 0, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", 0, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", 0, err
	}
	sealed := gcm.Seal(nil, nonce, plain, nil)

	sum := sha256.Sum256(key)
	keyID = hex.EncodeToString(sum[:keyIDLen])

	out := make([]byte, 0, headerLen+len(sealed))
	out = append(out, encMagic[:]...)
	out = append(out, encVersion)
	out = append(out, sum[:keyIDLen]...)
	out = append(out, nonce...)
	out = append(out, sealed...)

	// 原子写入：先写临时文件再重命名，避免半成品密文被保留策略误删/误用。
	tmp := dstPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return "", "", 0, err
	}
	if err := os.Rename(tmp, dstPath); err != nil {
		_ = os.Remove(tmp)
		return "", "", 0, err
	}
	h := sha256.Sum256(out)
	return keyID, hex.EncodeToString(h[:]), int64(len(out)), nil
}

// DecryptFile 解密 srcPath 到 dstPath。密钥指纹不一致返回 ErrKeyMismatch。
func DecryptFile(srcPath, dstPath string, key []byte) (keyID string, err error) {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}
	if len(raw) < headerLen || string(raw[:8]) != string(encMagic[:]) {
		return "", ErrBadFormat
	}
	if raw[8] != encVersion {
		return "", fmt.Errorf("%w: unsupported version %d", ErrBadFormat, raw[8])
	}
	sum := sha256.Sum256(key)
	keyID = hex.EncodeToString(sum[:keyIDLen])
	if string(raw[9:17]) != string(sum[:keyIDLen]) {
		return keyID, ErrKeyMismatch
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return keyID, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return keyID, err
	}
	nonce := raw[17:29]
	sealed := raw[headerLen:]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return keyID, fmt.Errorf("decrypt failed (corrupted or wrong key): %w", err)
	}
	if err := os.WriteFile(dstPath, plain, 0o600); err != nil {
		return keyID, err
	}
	return keyID, nil
}

// HeaderInfo 读取加密备份头部（不解密），用于恢复演练前的快速鉴别。
type HeaderInfo struct {
	KeyID  string `json:"key_id"`
	Size   int64  `json:"size"`
	Hasher func(path string) (string, error)
}

// ReadKeyID 读取备份文件中的密钥指纹（用于不匹配提示）。
func ReadKeyID(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, headerLen)
	if _, err := io.ReadFull(f, head); err != nil {
		return "", ErrBadFormat
	}
	if string(head[:8]) != string(encMagic[:]) {
		return "", ErrBadFormat
	}
	return hex.EncodeToString(head[9:17]), nil
}
