package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/theme"
)

// ---- 主题管理（里程碑8：全部管理接口仅 admin；公开主题资产游客可加载） ----

// listThemes 已安装主题列表（admin）。
func (s *Server) listThemes(c *gin.Context) {
	if s.Themes == nil {
		ok(c, gin.H{"themes": []any{}})
		return
	}
	ok(c, gin.H{"themes": s.Themes.List()})
}

// uploadTheme 上传主题 ZIP（multipart file，可选 sha256 字段校验）。
func (s *Server) uploadTheme(c *gin.Context) {
	if s.Themes == nil {
		fail(c, http.StatusNotFound, "theme manager disabled")
		return
	}
	// 限制上传体积（主题 ZIP 上限 4 MiB）
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, theme.MaxZipSize+1<<20)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fail(c, http.StatusBadRequest, "missing file")
		return
	}
	defer file.Close()
	if header.Size > theme.MaxZipSize {
		fail(c, http.StatusBadRequest, "zip too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, theme.MaxZipSize+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "read upload failed")
		return
	}
	if len(data) > theme.MaxZipSize {
		fail(c, http.StatusBadRequest, "zip too large")
		return
	}
	wantSHA := strings.TrimSpace(c.PostForm("sha256"))
	th, err := s.Themes.Install(data, wantSHA)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	s.auditLog(c, "theme.upload", th.Name+"@"+th.Version)
	ok(c, gin.H{"theme": th})
}

// activateTheme 启用主题（admin）。
func (s *Server) activateTheme(c *gin.Context) {
	if s.Themes == nil {
		fail(c, http.StatusNotFound, "theme manager disabled")
		return
	}
	name := c.Param("name")
	if err := s.Themes.SetActive(name); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	s.auditLog(c, "theme.activate", name)
	ok(c, gin.H{"ok": true, "active": s.Themes.Active()})
}

// rollbackTheme 回滚主题到上一版本（admin）。
func (s *Server) rollbackTheme(c *gin.Context) {
	if s.Themes == nil {
		fail(c, http.StatusNotFound, "theme manager disabled")
		return
	}
	name := c.Param("name")
	if err := s.Themes.Rollback(name); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	s.auditLog(c, "theme.rollback", name)
	ok(c, gin.H{"ok": true})
}

// deleteTheme 删除主题；当前启用主题先切默认再删除（admin）。
func (s *Server) deleteTheme(c *gin.Context) {
	if s.Themes == nil {
		fail(c, http.StatusNotFound, "theme manager disabled")
		return
	}
	name := c.Param("name")
	if err := s.Themes.Delete(name); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	s.auditLog(c, "theme.delete", name)
	ok(c, gin.H{"ok": true, "active": s.Themes.Active()})
}

// listThemeMarket 市场主题列表（admin）。
func (s *Server) listThemeMarket(c *gin.Context) {
	if s.Themes == nil {
		ok(c, gin.H{"themes": []any{}})
		return
	}
	ok(c, gin.H{"themes": s.Themes.ListMarket()})
}

// installThemeMarket 从市场安装主题（HTTPS + SHA-256 staging 后原子安装）。
func (s *Server) installThemeMarket(c *gin.Context) {
	if s.Themes == nil {
		fail(c, http.StatusNotFound, "theme manager disabled")
		return
	}
	name := c.Param("name")
	if err := s.Themes.InstallFromMarket(name); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	s.auditLog(c, "theme.market_install", name)
	ok(c, gin.H{"ok": true})
}

// themeAsset 公开主题静态资源（入口 CSS / 图片 / 字体；无鉴权，游客可加载）。
// 主题包在安装时已通过扩展名白名单校验，此处再次校验并加安全响应头。
func (s *Server) themeAsset(c *gin.Context) {
	if s.Themes == nil {
		writeTheme404(c)
		return
	}
	parts := strings.Split(strings.TrimPrefix(c.Param("filepath"), "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		writeTheme404(c)
		return
	}
	name := parts[0]
	rel := strings.Join(parts[1:], "/")
	data, err := s.Themes.OpenAsset(name, rel)
	if err != nil {
		writeTheme404(c)
		return
	}
	// 强化响应头：主题资源仅作样式/图片使用，禁止脚本执行
	c.Header("Content-Type", themeContentType(rel))
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self' data:")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(data)
}

func writeTheme404(c *gin.Context) {
	c.Status(http.StatusNotFound)
}

func themeContentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(name, ".gif"):
		return "image/gif"
	case strings.HasSuffix(name, ".webp"):
		return "image/webp"
	case strings.HasSuffix(name, ".ico"):
		return "image/x-icon"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(name, ".woff"):
		return "font/woff"
	default:
		return "application/octet-stream"
	}
}
