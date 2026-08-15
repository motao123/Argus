package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

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
}

// canAccessServer 校验身份能否访问该服务器（JWT 用户 owner 或 admin；PAT 白名单）。
func (s *Server) canAccessServer(serverID int64, c *gin.Context) bool {
	p := principalFromContext(c)
	if p == nil {
		return false
	}
	if p.IsAdmin {
		return true
	}
	if p.IsPAT {
		return p.canAccessServer(serverID)
	}
	// 普通用户：查服务器 owner
	var srv model.Server
	if err := s.DB.First(&srv, serverID).Error; err != nil {
		return false
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
