package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ---- 统一响应壳（借鉴 nezha 的 CommonResponse + 分页）----

// ok 成功响应：{"success":true,"data":{...}}
func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// okPage 分页成功响应：附 pagination{offset,limit,total}
func okPage(c *gin.Context, data any, total int64, offset, limit int) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
		"pagination": gin.H{
			"offset": offset,
			"limit":  limit,
			"total":  total,
		},
	})
}

// fail 失败响应：{"success":false,"error":"...","code":"..."}
// apiCode 为可选的稳定错误码（如 "server.offline"），前端可按 code 翻译；
// 未提供时不输出 code 字段，兼容旧调用与旧客户端。
func fail(c *gin.Context, code int, msg string, apiCode ...string) {
	body := gin.H{"success": false, "error": msg}
	if len(apiCode) > 0 && apiCode[0] != "" {
		body["code"] = apiCode[0]
	}
	c.JSON(code, body)
}

// ---- 统一错误 ----

type apiError struct {
	Code int
	Msg  string
}

func (e *apiError) Error() string { return e.Msg }

func badRequest(msg string) *apiError { return &apiError{http.StatusBadRequest, msg} }
func notFound(msg string) *apiError   { return &apiError{http.StatusNotFound, msg} }
func conflict(msg string) *apiError   { return &apiError{http.StatusConflict, msg} }
func internal(msg string) *apiError   { return &apiError{http.StatusInternalServerError, msg} }

// ---- 分页参数 ----

const (
	defaultPageSize = 50
	maxPageSize     = 500
)

// pagination 解析 offset/limit 查询参数。
func pagination(c *gin.Context) (offset, limit int) {
	offset = parseIntQuery(c, "offset", 0)
	limit = parseIntQuery(c, "limit", defaultPageSize)
	if limit > maxPageSize {
		limit = maxPageSize
	}
	if limit < 1 {
		limit = defaultPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return offset, limit
}

func parseIntQuery(c *gin.Context, key string, def int) int {
	v, ok := c.GetQuery(key)
	if !ok {
		return def
	}
	n := 0
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
