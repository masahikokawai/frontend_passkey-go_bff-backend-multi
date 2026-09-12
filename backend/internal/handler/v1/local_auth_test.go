package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

type fakeLocalAuthService struct {
	result service.LocalAuthResult
	err    error
}

func (f fakeLocalAuthService) VerifyLocalPassword(ctx context.Context, email, password string) (service.LocalAuthResult, error) {
	return f.result, f.err
}

func TestLocalAuthHandler_VerifyPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)

	post := func(h *LocalAuthHandler, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/auth/verify-local-password", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.VerifyPassword(c)
		return w
	}

	t.Run("検証成功時は200でuser_id/name/email/rolesを返す", func(t *testing.T) {
		h := NewLocalAuthHandler(fakeLocalAuthService{
			result: service.LocalAuthResult{UserID: 1, Name: "ローカル太郎", Email: "local@example.com", Roles: []string{"general"}},
		})
		w := post(h, `{"email":"local@example.com","password":"password"}`)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("Unmarshal失敗: %v", err)
		}
		if body["user_id"] != float64(1) {
			t.Errorf("user_id = %v, want 1", body["user_id"])
		}
		if body["email"] != "local@example.com" {
			t.Errorf("email = %v, want local@example.com", body["email"])
		}
	})

	t.Run("ErrInvalidCredentialsは401 invalid_credentials", func(t *testing.T) {
		h := NewLocalAuthHandler(fakeLocalAuthService{err: service.ErrInvalidCredentials})
		w := post(h, `{"email":"local@example.com","password":"wrong"}`)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		if !strings.Contains(w.Body.String(), "invalid_credentials") {
			t.Errorf("body = %s, want invalid_credentials", w.Body.String())
		}
	})

	t.Run("ErrPasswordExpiredは401 password_expired(invalid_credentialsに丸めない)", func(t *testing.T) {
		h := NewLocalAuthHandler(fakeLocalAuthService{err: service.ErrPasswordExpired})
		w := post(h, `{"email":"local@example.com","password":"password"}`)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		if !strings.Contains(w.Body.String(), "password_expired") {
			t.Errorf("body = %s, want password_expired", w.Body.String())
		}
	})

	t.Run("その他のエラーもinvalid_credentialsに丸める", func(t *testing.T) {
		h := NewLocalAuthHandler(fakeLocalAuthService{err: errors.New("unexpected")})
		w := post(h, `{"email":"local@example.com","password":"password"}`)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		if !strings.Contains(w.Body.String(), "invalid_credentials") {
			t.Errorf("body = %s, want invalid_credentials", w.Body.String())
		}
	})

	t.Run("emailが無いと400", func(t *testing.T) {
		h := NewLocalAuthHandler(fakeLocalAuthService{})
		w := post(h, `{"password":"password"}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}

func TestRequireLocalAuthInternalToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newRouter := func(expected string) *gin.Engine {
		r := gin.New()
		r.Use(RequireLocalAuthInternalToken(expected))
		r.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
		return r
	}

	t.Run("ヘッダが無いと401", func(t *testing.T) {
		r := newRouter("secret")
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("ヘッダが違うと401", func(t *testing.T) {
		r := newRouter("secret")
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.Header.Set("X-Local-Auth-Internal-Token", "wrong")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("ヘッダが一致すると次のハンドラへ進む", func(t *testing.T) {
		r := newRouter("secret")
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.Header.Set("X-Local-Auth-Internal-Token", "secret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})
}
