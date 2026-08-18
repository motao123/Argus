package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifyctx"
	"github.com/motao123/Argus/server/internal/retention"
)

// 站点设置键
const (
	SettingSiteName         = "site_name"
	SettingSiteDesc         = "site_desc"
	SettingFavicon          = "favicon"
	SettingForceAuth        = "force_auth"         // 1 = 强制登录
	SettingTermFontSize     = "term_font_size"     // 终端字号（默认 13）
	SettingTermTheme        = "term_theme"         // 终端主题：dark/light
	SettingCustomCSS        = "custom_css"         // 注入两站 <head> 的 CSS
	SettingCustomJS         = "custom_js"          // 注入两站 </body> 前的 JS
	SettingCustomFooter     = "custom_footer"      // 前台页脚自定义 HTML
	SettingExpireNotifyDays = "expire_notify_days" // 到期提前提醒天数（默认 3，范围 1-30）
	// 通知/分享
	SettingMaskIP             = "mask_ip"                 // 1 = 通知中隐藏服务器 IP（借鉴 nezha EnablePlainIPInNotification）
	SettingLoginNotifyWebhook = "login_notify_webhook_id" // 登录成功通知渠道（0 = 不通知）
	SettingTempShareKey       = "temp_share_key"          // 临时分享密钥（SHA-256；私有站点模式下可凭 ?temp_key= 访问公开接口）
	SettingTempShareExpiresAt = "temp_share_expires_at"   // 临时分享密钥过期时间（RFC3339；空 = 永久）
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
	activeTheme := "default"
	activeEntry := ""
	if s.Themes != nil {
		activeTheme = s.Themes.Active()
		activeEntry = s.Themes.ActiveEntry()
	}
	ok(c, gin.H{
		"site_name":          s.GetSetting(SettingSiteName, "Argus"),
		"site_desc":          s.GetSetting(SettingSiteDesc, "轻量自托管服务器监控"),
		"favicon":            s.GetSetting(SettingFavicon, ""),
		"force_auth":         s.GetSetting(SettingForceAuth, "0") == "1",
		"active_theme":       activeTheme,
		"active_theme_entry": activeEntry,
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
		// 临时分享密钥不回显（存储为哈希，回读空串；留空保存 = 保持不变）
		if st.Key == SettingTempShareKey {
			continue
		}
		m[st.Key] = st.Value
	}
	m[SettingExpireNotifyDays] = s.GetSetting(SettingExpireNotifyDays, "3")
	m[SettingTempShareKey] = ""
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
	if raw, ok := req.Settings[SettingExpireNotifyDays]; ok {
		if v, err := strconv.Atoi(raw); err != nil || v < 1 || v > 30 {
			fail(c, http.StatusBadRequest, "expire_notify_days must be an integer between 1 and 30")
			return
		}
	}
	// 临时分享密钥：保存前哈希（存储 SHA-256，不落明文；校验时同样哈希比对）。
	// 空串 = 保持不变（不回显，避免误清空；设置新密钥需输入非空值）。
	if raw, ok := req.Settings[SettingTempShareKey]; ok {
		if raw == "" {
			delete(req.Settings, SettingTempShareKey)
		} else {
			sum := sha256.Sum256([]byte(raw))
			req.Settings[SettingTempShareKey] = hex.EncodeToString(sum[:])
		}
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
	s.refreshNotifySettings()
	s.auditLog(c, "settings.update", "")
	ok(c, gin.H{"ok": true})
}

// refreshNotifySettings 根据站点设置刷新通知相关全局开关（IP 打码）。
func (s *Server) refreshNotifySettings() {
	notifyctx.SetMaskIP(s.GetSetting(SettingMaskIP, "0") == "1")
}
