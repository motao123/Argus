package task

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/motao123/Argus/protocol"
)

// ---- 文件管理（借鉴 nezha Fs* 任务）----

func (h *Handler) handleFsList(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.FsListParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	path := p.Path
	if path == "" {
		path = "."
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return &protocol.FsListResult{Path: p.Path, Error: err.Error()}, nil
	}
	out := make([]protocol.FsEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, protocol.FsEntry{
			Name:     e.Name(),
			Path:     filepath.Join(path, e.Name()),
			Size:     info.Size(),
			Mode:     info.Mode().String(),
			IsDir:    e.IsDir(),
			Modified: info.ModTime().Unix(),
		})
	}
	return &protocol.FsListResult{Path: path, Entries: out}, nil
}

func (h *Handler) handleFsRead(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.FsReadParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	f, err := os.Open(p.Path)
	if err != nil {
		return &protocol.FsReadResult{Error: err.Error()}, nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return &protocol.FsReadResult{Error: err.Error()}, nil
	}
	limit := p.Limit
	if limit <= 0 || limit > 4*1024*1024 {
		limit = 256 * 1024
	}
	buf := make([]byte, limit)
	if p.Offset > 0 {
		if _, err := f.Seek(p.Offset, 0); err != nil {
			return &protocol.FsReadResult{Error: err.Error()}, nil
		}
	}
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return &protocol.FsReadResult{EOF: true, Size: info.Size(), Error: err.Error()}, nil
	}
	return &protocol.FsReadResult{
		Data: append([]byte(nil), buf[:n]...),
		EOF:  int64(n) < int64(limit) || p.Offset+int64(n) >= info.Size(),
		Size: info.Size(),
	}, nil
}

func (h *Handler) handleFsWrite(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.FsWriteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	flags := os.O_CREATE | os.O_WRONLY
	if p.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return &protocol.FsWriteResult{Error: err.Error()}, nil
	}
	f, err := os.OpenFile(p.Path, flags, 0o644)
	if err != nil {
		return &protocol.FsWriteResult{Error: err.Error()}, nil
	}
	defer f.Close()
	n, err := f.Write(p.Data)
	if err != nil {
		return &protocol.FsWriteResult{Bytes: n, Error: err.Error()}, nil
	}
	return &protocol.FsWriteResult{Bytes: n}, nil
}

func (h *Handler) handleFsDelete(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.FsDeleteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	var err error
	if p.Recursive {
		err = os.RemoveAll(p.Path)
	} else {
		err = os.Remove(p.Path)
	}
	if err != nil {
		return &protocol.FsDeleteResult{Error: err.Error()}, nil
	}
	return &protocol.FsDeleteResult{}, nil
}
