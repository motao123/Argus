package main

import (
	"embed"
	"encoding/json"
	"io"
)

//go:embed webdist
var webdistFS embed.FS

//go:embed install.sh
var installScript []byte

// embeddedFS 前端产物文件系统；webdist 目录不存在时返回 nil。
var embeddedFS = loadEmbedded()

func loadEmbedded() embedFS {
	if _, err := webdistFS.ReadFile("webdist/index.html"); err != nil {
		return nil
	}
	return embedFSImpl{fs: webdistFS}
}

type embedFS interface {
	ReadFile(name string) ([]byte, error)
}

type embedFSImpl struct{ fs embed.FS }

func (e embedFSImpl) ReadFile(name string) ([]byte, error) {
	return e.fs.ReadFile("webdist/" + name)
}

func encodeJSON(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}
