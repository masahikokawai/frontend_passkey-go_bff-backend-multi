package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCSRFMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := CookieConfig{CSRFCookieName: "csrf_token", Secure: false}

	tests := []struct {
		name        string
		method      string
		cookieValue string
		headerValue string
		wantStatus  int
	}{
		{name: "GETはCSRF検証の対象外", method: http.MethodGet, wantStatus: http.StatusOK},
		{name: "POSTでCookie/ヘッダが一致すれば通過", method: http.MethodPost, cookieValue: "abc", headerValue: "abc", wantStatus: http.StatusOK},
		{name: "POSTでCookieが無ければ403", method: http.MethodPost, cookieValue: "", headerValue: "abc", wantStatus: http.StatusForbidden},
		{name: "POSTでヘッダが無ければ403", method: http.MethodPost, cookieValue: "abc", headerValue: "", wantStatus: http.StatusForbidden},
		{name: "POSTで値が不一致なら403", method: http.MethodPost, cookieValue: "abc", headerValue: "xyz", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(CSRFMiddleware(cfg))
			router.Handle(tt.method, "/test", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(tt.method, "/test", nil)
			if tt.cookieValue != "" {
				req.AddCookie(&http.Cookie{Name: cfg.CSRFCookieName, Value: tt.cookieValue})
			}
			if tt.headerValue != "" {
				req.Header.Set("X-CSRF-Token", tt.headerValue)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// CONTRACT.mdセクション22.5: パスキーのlogin/begin・login/finishはログイン前(まだ
// csrf_token Cookieが無い状態)に呼ばれるため、ローカルログインと同じくCSRF検証の対象外にする
func TestCSRFMiddleware_パスキーのlogin系パスはCookie無しでも通過する(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := CookieConfig{CSRFCookieName: "csrf_token", Secure: false}

	for _, path := range []string{"/api/auth/passkey/login/begin", "/api/auth/passkey/login/finish"} {
		t.Run(path, func(t *testing.T) {
			router := gin.New()
			router.Use(CSRFMiddleware(cfg))
			router.POST(path, func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodPost, path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200 (csrf_token cookie無しでも通過するはず), body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// register/begin・register/finishは要ログイン(通常のCSRF検証対象)であることの回帰確認
func TestCSRFMiddleware_パスキーのregister系パスはCSRF検証の対象のまま(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := CookieConfig{CSRFCookieName: "csrf_token", Secure: false}

	router := gin.New()
	router.Use(CSRFMiddleware(cfg))
	router.POST("/api/auth/passkey/register/begin", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/begin", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403(csrf_token Cookie無しのため), body=%s", rec.Code, rec.Body.String())
	}
}
