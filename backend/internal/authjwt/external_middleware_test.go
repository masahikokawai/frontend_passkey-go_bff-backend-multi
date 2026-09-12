package authjwt

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireExternalClientAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwks := newTestJWKSServer(t)
	priv := jwks.addKey(t, "kid-ext")
	const issuer = "http://keycloak.test/realms/training"
	const audience = "backend"
	verifier := NewVerifier(jwks.server.URL, issuer, audience)

	newRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(RequireExternalClientAuth(verifier, "external-api-client"))
		r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
		return r
	}

	t.Run("azpが期待するクライアントIDと異なると403", func(t *testing.T) {
		r := newRouter()
		claims := baseClaims(issuer, audience)
		claims["azp"] = "someone-else"
		token := mintToken(t, priv, "kid-ext", claims)
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})

	t.Run("azpが一致すれば通過する", func(t *testing.T) {
		r := newRouter()
		claims := baseClaims(issuer, audience)
		claims["azp"] = "external-api-client"
		token := mintToken(t, priv, "kid-ext", claims)
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})

	t.Run("不正なトークンは401", func(t *testing.T) {
		r := newRouter()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("Authorization", "Bearer invalid")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("Authorizationヘッダが無いと401", func(t *testing.T) {
		r := newRouter()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})
}
