package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
)

// listFiles 列目录（agent 执行）。
func (s *Server) listFiles(c *gin.Context) {
	serverID := mustIDParam(c, "serverId")
	if !s.canAccessServer(serverID, c) {
		fail(c, http.StatusForbidden, "not in token whitelist")
		return
	}
	path := c.DefaultQuery("path", ".")
	resp, err := s.Agents.Call(serverID, protocol.MethodFsList, protocol.FsListParams{Path: path})
	if err != nil {
		fail(c, http.StatusConflict, err.Error())
		return
	}
	if resp.Error != nil {
		fail(c, http.StatusBadGateway, resp.Error.Message)
		return
	}
	var result protocol.FsListResult
	if err := decodeRPCResult(resp.Result, &result); err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	if result.Error != "" {
		fail(c, http.StatusBadGateway, result.Error)
		return
	}
	ok(c, result)
}

// readFile 读文件分片。
func (s *Server) readFile(c *gin.Context) {
	serverID := mustIDParam(c, "serverId")
	if !s.canAccessServer(serverID, c) {
		fail(c, http.StatusForbidden, "not in token whitelist")
		return
	}
	var req struct {
		Path   string `json:"path"`
		Offset int64  `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	resp, err := s.Agents.Call(serverID, protocol.MethodFsRead, protocol.FsReadParams{
		Path: req.Path, Offset: req.Offset, Limit: req.Limit,
	})
	if err != nil {
		fail(c, http.StatusConflict, err.Error())
		return
	}
	if resp.Error != nil {
		fail(c, http.StatusBadGateway, resp.Error.Message)
		return
	}
	var result protocol.FsReadResult
	if err := decodeRPCResult(resp.Result, &result); err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	if result.Error != "" {
		fail(c, http.StatusBadGateway, result.Error)
		return
	}
	ok(c, gin.H{
		"data": base64.StdEncoding.EncodeToString(result.Data),
		"eof":  result.EOF,
		"size": result.Size,
	})
}

// writeFile 写文件（base64 数据）。
func (s *Server) writeFile(c *gin.Context) {
	serverID := mustIDParam(c, "serverId")
	if !s.canAccessServer(serverID, c) {
		fail(c, http.StatusForbidden, "not in token whitelist")
		return
	}
	var req struct {
		Path   string `json:"path"`
		Data   string `json:"data"` // base64
		Append bool   `json:"append"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid base64 data")
		return
	}
	resp, err := s.Agents.Call(serverID, protocol.MethodFsWrite, protocol.FsWriteParams{
		Path: req.Path, Data: data, Append: req.Append,
	})
	if err != nil {
		fail(c, http.StatusConflict, err.Error())
		return
	}
	if resp.Error != nil {
		fail(c, http.StatusBadGateway, resp.Error.Message)
		return
	}
	var result protocol.FsWriteResult
	if err := decodeRPCResult(resp.Result, &result); err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	if result.Error != "" {
		fail(c, http.StatusBadGateway, result.Error)
		return
	}
	ok(c, result)
}

// uploadFile 流式上传文件。保留原有 JSON write 接口，避免旧客户端行为变化。
func (s *Server) uploadFile(c *gin.Context) {
	serverID := mustIDParam(c, "serverId")
	if !s.canAccessServer(serverID, c) {
		fail(c, http.StatusForbidden, "not in token whitelist")
		return
	}
	path := strings.TrimSpace(c.Query("path"))
	if path == "" {
		fail(c, http.StatusBadRequest, "path required")
		return
	}
	reader, err := c.Request.MultipartReader()
	if err != nil {
		fail(c, http.StatusBadRequest, "multipart upload required")
		return
	}
	const chunkSize = 256 * 1024
	buf := make([]byte, chunkSize)
	var written int64
	filename := ""
	foundFile := false
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			fail(c, http.StatusBadRequest, "failed to read multipart upload")
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}
		foundFile = true
		filename = part.FileName()
		written, err = streamFileChunks(part, buf, func(data []byte, appendChunk bool) error {
			resp, callErr := s.Agents.Call(serverID, protocol.MethodFsWrite, protocol.FsWriteParams{
				Path: path, Data: data, Append: appendChunk,
			})
			if callErr != nil {
				return callErr
			}
			if resp.Error != nil {
				return errors.New(resp.Error.Message)
			}
			var result protocol.FsWriteResult
			if err := decodeRPCResult(resp.Result, &result); err != nil {
				return err
			}
			if result.Error != "" {
				return errors.New(result.Error)
			}
			return nil
		})
		part.Close()
		if err != nil {
			fail(c, http.StatusBadGateway, err.Error())
			return
		}
		break
	}
	if !foundFile {
		fail(c, http.StatusBadRequest, "file required")
		return
	}
	s.auditLog(c, "file.upload", filepath.Base(path))
	ok(c, gin.H{"bytes": written, "name": filename})
}

func streamFileChunks(reader io.Reader, buf []byte, write func(data []byte, appendChunk bool) error) (int64, error) {
	if len(buf) == 0 {
		return 0, errors.New("empty upload buffer")
	}
	appendChunk := false
	var written int64
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if writeErr := write(append([]byte(nil), buf[:n]...), appendChunk); writeErr != nil {
				return written, writeErr
			}
			written += int64(n)
			appendChunk = true
		}
		if err != nil {
			if err != io.EOF {
				return written, err
			}
			break
		}
	}
	if written == 0 {
		if err := write([]byte{}, false); err != nil {
			return 0, err
		}
	}
	return written, nil
}

// deleteFile 删除文件/目录。
func (s *Server) deleteFile(c *gin.Context) {
	serverID := mustIDParam(c, "serverId")
	if !s.canAccessServer(serverID, c) {
		fail(c, http.StatusForbidden, "not in token whitelist")
		return
	}
	var req struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	resp, err := s.Agents.Call(serverID, protocol.MethodFsDelete, protocol.FsDeleteParams{
		Path: req.Path, Recursive: req.Recursive,
	})
	if err != nil {
		fail(c, http.StatusConflict, err.Error())
		return
	}
	if resp.Error != nil {
		fail(c, http.StatusBadGateway, resp.Error.Message)
		return
	}
	var result protocol.FsDeleteResult
	if err := decodeRPCResult(resp.Result, &result); err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	if result.Error != "" {
		fail(c, http.StatusBadGateway, result.Error)
		return
	}
	ok(c, gin.H{"ok": true})
	s.auditLog(c, "file.delete", req.Path)
}

// canAccessServer 校验身份能否访问该服务器（JWT 用户 owner 或 admin；PAT 白名单）。
func (s *Server) canAccessServer(serverID int64, c *gin.Context) bool {
	p := principalFromContext(c)
	if p == nil {
		return false
	}
	var srv model.Server
	if err := s.DB.First(&srv, serverID).Error; err != nil {
		return false
	}
	if p.IsPAT {
		// PAT 仍受白名单约束，且不能借白名单跨越服务器 owner 边界。
		return p.canAccessServer(serverID) && (p.IsAdmin || srv.OwnerID == p.UserID)
	}
	if p.IsAdmin {
		return true
	}
	return srv.OwnerID == p.UserID
}

func decodeRPCResult(result any, out any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
