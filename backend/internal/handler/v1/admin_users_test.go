package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestRequireAdminInternalToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		gotHeader  string
		wantStatus int
	}{
		{"正しいトークンなら次へ進む", "expected-token", http.StatusOK},
		{"トークンが空なら401", "", http.StatusUnauthorized},
		{"トークンが不一致なら401", "wrong-token", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/x", RequireAdminInternalToken("expected-token"), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tt.gotHeader != "" {
				req.Header.Set("X-Admin-Internal-Token", tt.gotHeader)
			}
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestUserHandler_CreateAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("正常な入力なら201でuser_idを返す", func(t *testing.T) {
		h := NewUserHandler(service.NewUserService(newFakeUserServiceRepo()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/admin/users",
			strings.NewReader(`{"name":"花子","email":"hanako@example.com","password":"password123","role":"general"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		h.CreateAdmin(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201, body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("Unmarshal失敗: %v", err)
		}
		if body["email"] != "hanako@example.com" {
			t.Errorf("email = %v, want hanako@example.com", body["email"])
		}
	})

	t.Run("roleが不正なら422", func(t *testing.T) {
		h := NewUserHandler(service.NewUserService(newFakeUserServiceRepo()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/admin/users",
			strings.NewReader(`{"name":"花子","email":"hanako@example.com","password":"password123","role":"unknown"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		h.CreateAdmin(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422, body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("emailが重複していれば422 email_taken", func(t *testing.T) {
		repo := newFakeUserServiceRepo()
		userService := service.NewUserService(repo)
		if _, err := userService.Create(context.Background(), "既存", "dup@example.com", "password123", model.RoleGeneral); err != nil {
			t.Fatalf("前提のCreate失敗: %v", err)
		}
		h := NewUserHandler(userService)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/admin/users",
			strings.NewReader(`{"name":"次郎","email":"dup@example.com","password":"password123","role":"general"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		h.CreateAdmin(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422, body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		json.Unmarshal(w.Body.Bytes(), &body)
		if body["error"] != "email_taken" {
			t.Errorf("error = %v, want email_taken", body["error"])
		}
		// 【実機検証で発覚した回帰防止】
		// 以前はここに "message" キーが無く、admin/go・admin/rails の画面に空のエラーメッセージが表示されていた
		if msg, _ := body["message"].(string); msg == "" {
			t.Errorf("message が空、または存在しない: body=%v", body)
		}
	})

	t.Run("必須項目が無いbindingエラーなら422", func(t *testing.T) {
		h := NewUserHandler(service.NewUserService(newFakeUserServiceRepo()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/admin/users", strings.NewReader(`{"name":"花子"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		h.CreateAdmin(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422, body=%s", w.Code, w.Body.String())
		}
	})
}

func TestUserHandler_DeleteAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("削除に成功すれば204", func(t *testing.T) {
		repo := newFakeUserServiceRepo()
		userService := service.NewUserService(repo)
		if _, err := userService.Provision(context.Background(), "sub-1", "太郎", "taro@example.com", nil); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}
		h := NewUserHandler(userService)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodDelete, "/internal/v1/admin/users/1", nil)
		c.Params = gin.Params{{Key: "id", Value: "1"}}

		h.DeleteAdmin(c)
		// c.Status()だけを呼ぶハンドラ(bodyを書かない204応答)は、Ginの
		// responseWriterがヘッダ書き込みを遅延させる実装のため、router.ServeHTTP経由の
		// フルフローを通さずハンドラ関数を直接呼ぶこのテストパターンでは、
		// 明示的にWriteHeaderNow()を呼ばない限りhttptest.ResponseRecorder.Codeが
		// 既定値200のまま更新されない(実際にこのテストで踏んだ)
		c.Writer.WriteHeaderNow()

		if w.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204, body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("最後の管理者は削除できず409", func(t *testing.T) {
		repo := newFakeUserServiceRepo()
		userService := service.NewUserService(repo)
		if _, err := userService.Provision(context.Background(), "admin-sub", "管理者", "admin@example.com", []string{"management"}); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}
		h := NewUserHandler(userService)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodDelete, "/internal/v1/admin/users/1", nil)
		c.Params = gin.Params{{Key: "id", Value: "1"}}

		h.DeleteAdmin(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422(ErrLastManagerUser、renderServiceErrorの既存マッピングをUpdateRoleと共有), body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("存在しないIDは404", func(t *testing.T) {
		h := NewUserHandler(service.NewUserService(newFakeUserServiceRepo()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodDelete, "/internal/v1/admin/users/999", nil)
		c.Params = gin.Params{{Key: "id", Value: "999"}}

		h.DeleteAdmin(c)

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404, body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("idがuint64でなければ400", func(t *testing.T) {
		h := NewUserHandler(service.NewUserService(newFakeUserServiceRepo()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodDelete, "/internal/v1/admin/users/abc", nil)
		c.Params = gin.Params{{Key: "id", Value: "abc"}}

		h.DeleteAdmin(c)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400, body=%s", w.Code, w.Body.String())
		}
	})
}
