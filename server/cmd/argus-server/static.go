package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// injectHTML 由 main 注入：从 DB 读自定义代码并改写 HTML 响应（自定义 CSS/JS/页脚）。
var injectHTML func(html string) string

// serveEmbedded 提供前端静态资源（构建时注入 web/dist 产物）。
// 若 embed 目录不存在（纯 API 开发模式），返回 404 JSON。
func serveEmbedded(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if embeddedFS == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "frontend not embedded (build with `make build` to include web/dist)",
		})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" || path == "index.html" {
		serveFile(w, r, "index.html")
		return
	}
	if strings.HasPrefix(path, "assets/") {
		serveFile(w, r, path)
		return
	}
	// SPA 路由回退到 index.html
	serveFile(w, r, "index.html")
}

func serveFile(w http.ResponseWriter, r *http.Request, name string) {
	data, err := embeddedFS.ReadFile(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	body := string(data)
	if injectHTML != nil && strings.HasSuffix(name, ".html") {
		body = injectHTML(body)
	}
	w.Header().Set("Content-Type", contentType(name))
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(body))
}

func contentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".ico"):
		return "image/x-icon"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
