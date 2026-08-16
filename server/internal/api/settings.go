package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/retention"
)

// 站点设置键
const (
	SettingSiteName     = "site_name"
	SettingSiteDesc     = "site_desc"
	SettingFavicon      = "favicon"
	SettingForceAuth    = "force_auth"     // 1 = 强制登录
	SettingTermFontSize = "term_font_size" // 终端字号（默认 13）
	SettingTermTheme    = "term_theme"     // 终端主题：dark/light
	SettingCustomCSS    = "custom_css"     // 注入两站 <head> 的 CSS
	SettingCustomJS     = "custom_js"      // 注入两站 </body> 前的 JS
	SettingCustomFooter = "custom_footer"  // 前台页脚自定义 HTML
)

// GetSetting 读设置（默认值兜底）。
func (s *Server) GetSetting(key, def string) string {
	var st model.Setting
	if err := s.DB.Where("key = ?", key).First(&st).Error; err != nil {
		return def
	}
	if st.Value == "" {
		return def
	}
	return st.Value
}

// getPublicSettings 公开设置（前台/游客可读）。
func (s *Server) getPublicSettings(c *gin.Context) {
	ok(c, gin.H{
		"site_name":  s.GetSetting(SettingSiteName, "Argus"),
		"site_desc":  s.GetSetting(SettingSiteDesc, "轻量自托管服务器监控"),
		"favicon":    s.GetSetting(SettingFavicon, ""),
		"force_auth": s.GetSetting(SettingForceAuth, "0") == "1",
	})
}

// getTermSettings 终端外观设置（公开可读，终端页使用）。
func (s *Server) getTermSettings(c *gin.Context) {
	fontSize := s.GetSetting(SettingTermFontSize, "13")
	theme := s.GetSetting(SettingTermTheme, "dark")
	ok(c, gin.H{"font_size": fontSize, "theme": theme})
}

// getSettings 管理端读取全部设置。
func (s *Server) getSettings(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var settings []model.Setting
	s.DB.Find(&settings)
	m := retention.SettingDefaults()
	for _, st := range settings {
		m[st.Key] = st.Value
	}
	ok(c, gin.H{"settings": m})
}

// saveSettings 保存设置（admin）。
func (s *Server) saveSettings(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		Settings map[string]string `json:"settings"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if err := retention.ValidateSettings(req.Settings); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	for k, v := range req.Settings {
		var st model.Setting
		err := s.DB.Where("key = ?", k).First(&st).Error
		if err == nil {
			s.DB.Model(&st).Update("value", v)
		} else {
			_ = s.DB.Create(&model.Setting{Key: k, Value: v}).Error
		}
	}
	s.auditLog(c, "settings.update", "")
	ok(c, gin.H{"ok": true})
}
