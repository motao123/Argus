package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
)

// listUsers 用户列表（仅 admin）。
func (s *Server) listUsers(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var users []model.User
	if err := s.DB.Order("id").Find(&users).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"users": users})
}

// getUserSecret 读取用户专属 Agent 密钥（仅 admin，借鉴 nezha 管理端读取 agent_secret）。
func (s *Server) getUserSecret(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var u model.User
	if err := s.DB.First(&u, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	ok(c, gin.H{"agent_secret": u.AgentSecret})
}

// createUser 创建用户（仅 admin），返回用户专属 Agent 密钥。
func (s *Server) createUser(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 6 {
		fail(c, http.StatusBadRequest, "username required, password >= 6 chars")
		return
	}
	// 合法角色：admin / user / readonly（历史数据默认 user 兼容）
	if !model.IsValidRole(req.Role) {
		req.Role = model.RoleUser
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(c, http.StatusInternalServerError, "hash password")
		return
	}
	u := model.User{
		Username:     req.Username,
		PasswordHash: string(hash),
		Role:         req.Role,
		AgentSecret:  agent.GenSecret(),
	}
	if err := s.DB.Create(&u).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"user": u, "agent_secret": u.AgentSecret})
	s.auditLog(c, "user.create", u.Username)
}

// deleteUser 删除用户（仅 admin）。其名下服务器联动删除（借鉴 nezha）。
func (s *Server) deleteUser(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	if id == p.UserID {
		fail(c, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	// 级联：先删用户，再删其名下服务器
	var servers []model.Server
	s.DB.Where("owner_id = ?", id).Find(&servers)
	for i := range servers {
		if peer := s.Agents.Peer(servers[i].ID); peer != nil {
			_ = peer.Close()
		}
		s.Store.Remove(servers[i].ID)
	}
	if err := s.DB.Where("owner_id = ?", id).Delete(&model.Server{}).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.DB.Delete(&model.User{}, id).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "deleted_servers": len(servers)})
	s.auditLog(c, "user.delete", fmt.Sprintf("user_id=%d", id))
}

// updateUser 修改用户（admin 改角色；用户自己改密码）。
func (s *Server) updateUser(c *gin.Context) {
	p := principalFromContext(c)
	id := mustID(c)
	var req struct {
		Password *string `json:"password"`
		Role     *string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var u model.User
	if err := s.DB.First(&u, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	updates := map[string]any{}
	if req.Password != nil && *req.Password != "" {
		if id != p.UserID && !p.IsAdmin {
			fail(c, http.StatusForbidden, "not your account")
			return
		}
		if len(*req.Password) < 6 {
			fail(c, http.StatusBadRequest, "password >= 6 chars")
			return
		}
		hash, _ := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		updates["password_hash"] = string(hash)
	}
	if req.Role != nil {
		if !p.IsAdmin {
			fail(c, http.StatusForbidden, "admin only")
			return
		}
		if !model.IsValidRole(*req.Role) {
			fail(c, http.StatusBadRequest, "invalid role")
			return
		}
		updates["role"] = *req.Role
	}
	if len(updates) > 0 {
		if err := s.DB.Model(&u).Updates(updates).Error; err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ok(c, gin.H{"ok": true})
}
