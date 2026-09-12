package authjwt

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwks := newTestJWKSServer(t)
	priv := jwks.addKey(t, "kid-1")
	const issuer = "http://keycloak.test/realms/training"
	const audience = "backend"
	verifier := NewVerifier(jwks.server.URL, issuer, audience)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	newRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(RequireAuth(verifier, logger))
		r.GET("/ping", func(c *gin.Context) {
			claims, ok := ClaimsFromContext(c)
			if !ok {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "no claims"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"sub": claims.Subject})
		})
		return r
	}

	t.Run("Authorizationヘッダが無いと401", func(t *testing.T) {
		r := newRouter()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("不正なトークンは401", func(t *testing.T) {
		r := newRouter()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("Authorization", "Bearer not-a-real-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("正しいトークンは次のハンドラへ進みClaimsが取れる", func(t *testing.T) {
		r := newRouter()
		claims := baseClaims(issuer, audience)
		claims["sub"] = "user-42"
		token := mintToken(t, priv, "kid-1", claims)
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "user-42") {
			t.Errorf("bodyにsubが含まれていない: %s", w.Body.String())
		}
	})
}
