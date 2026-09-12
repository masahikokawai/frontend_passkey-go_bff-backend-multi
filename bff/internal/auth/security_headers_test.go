package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware自体は分岐の無い単純な処理だが、3回目のe2e監査で
// 「明示的なキャッシュ制御ヘッダーが無い」という指摘が実際に出たことを踏まえ、
// 各ヘッダーが確実に付与されることを回帰として固定しておく
func TestSecurityHeadersMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SecurityHeadersMiddleware())
	router.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	router.ServeHTTP(w, req)

	tests := []struct {
		header string
		want   string
	}{
		{"Cache-Control", "no-store"},
		{"Pragma", "no-cache"},
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
	}
	for _, tt := range tests {
		if got := w.Header().Get(tt.header); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.header, got, tt.want)
		}
	}
}
