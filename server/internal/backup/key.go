package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// KeyProvider 返回加密密钥材料及其来源标签。标签仅描述来源（env:/file:/jwt:），
// 密钥材料本身绝不写入数据库。
type KeyProvider func() (material []byte, source string, err error)

// DefaultKeyProvider 密钥材料解析优先级：
//  1. 环境变量 ARGUS_BACKUP_KEY（任意长度口令）
//  2. 环境变量 ARGUS_BACKUP_KEY_FILE 指向的文件内容
//  3. 兜底：复用 JWT 密钥文件（<dbPath>.jwt）——零配置可用，且与既有密钥存储一致
func DefaultKeyProvider(dbPath string) KeyProvider {
	return func() ([]byte, string, error) {
		if v := os.Getenv("ARGUS_BACKUP_KEY"); v != "" {
			return []byte(v), "env:ARGUS_BACKUP_KEY", nil
		}
		if p := os.Getenv("ARGUS_BACKUP_KEY_FILE"); p != "" {
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil, "", err
			}
			if len(strings.TrimSpace(string(raw))) == 0 {
				return nil, "", errors.New("backup key file is empty")
			}
			return raw, "file:" + p, nil
		}
		raw, err := os.ReadFile(dbPath + ".jwt")
		if err != nil {
			return nil, "", errors.New("no backup key: set ARGUS_BACKUP_KEY / ARGUS_BACKUP_KEY_FILE (fallback jwt secret file missing: " + err.Error() + ")")
		}
		if len(strings.TrimSpace(string(raw))) == 0 {
			return nil, "", errors.New("backup key fallback (jwt secret file) is empty")
		}
		return raw, "jwt:" + filepath.Base(dbPath) + ".jwt", nil
	}
}
