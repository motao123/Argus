package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// fakeClock 可推进的测试时钟。
type fakeClock struct {
	now time.Time
}

func (f *fakeClock) advance(d time.Duration) { f.now = f.now.Add(d) }

func TestWAFWindowResetAndBlock(t *testing.T) {
	clk := &fakeClock{now: time.Now()}
	lim := newWAF(5, time.Minute, 10*time.Minute)
	lim.now = func() time.Time { return clk.now }

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(lim.middleware())
	r.GET("/t", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 窗口内前 5 次通过
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d want 200", i+1, w.Code)
		}
	}
	// 第 6 次超限 → 429
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("over limit: got %d want 429", w.Code)
	}
	// 封禁期内继续 429
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked: got %d want 429", w.Code)
	}
	// 窗口推进但仍在封禁期 → 仍 429
	clk.advance(2 * time.Minute)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("still blocked: got %d want 429", w.Code)
	}
	// 封禁到期后恢复 200
	clk.advance(9 * time.Minute)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("after unblock: got %d want 200", w.Code)
	}
}
