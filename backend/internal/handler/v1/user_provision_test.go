package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// fakeUserServiceRepo はservice.NewUserServiceへ渡すための最小フェイクリポジトリ
// service.userRepositoryは非公開型だが、Goのインターフェースは構造的に満たされるため
// (パッケージが違ってもメソッドセットが一致すればよい)、ここで定義した型を
// そのままservice.NewUserServiceの引数として渡せる
//
// 【セクション16.2で変更】model.Userはkeycloak_subを持たなくなった(user_keycloaksへ分離)
// ため、フェイク内部だけで使うkeycloakSubByUserIDで対応関係を保持する
type fakeUserServiceRepo struct {
	byID            map[uint64]*model.User
	keycloakSubByID map[uint64]string
}

func newFakeUserServiceRepo() *fakeUserServiceRepo {
	return &fakeUserServiceRepo{byID: map[uint64]*model.User{}, keycloakSubByID: map[uint64]string{}}
}

func (f *fakeUserServiceRepo) UpsertByKeycloakSub(ctx context.Context, keycloakSub, email, name string, role model.Role) (*model.User, error) {
	for id, sub := range f.keycloakSubByID {
		if sub == keycloakSub {
			u := f.byID[id]
			u.Email, u.Name = email, name
			return u, nil
		}
	}
	id := uint64(len(f.byID) + 1)
	u := &model.User{ID: id, Email: email, Name: name, Role: role}
	f.byID[id] = u
	f.keycloakSubByID[id] = keycloakSub
	return u, nil
}

func (f *fakeUserServiceRepo) List(ctx context.Context) ([]model.User, error) {
	out := make([]model.User, 0, len(f.byID))
	for _, u := range f.byID {
		out = append(out, *u)
	}
	return out, nil
}

func (f *fakeUserServiceRepo) Get(ctx context.Context, id uint64) (*model.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return u, nil
}

// checkNotLastManagerLocked は本物の repository.User.checkNotLastManagerLocked と同じ判定ロジックをインメモリで再現したもの
// (TOCTOU 対策としての実際の行ロックは fake では意味を持たないため対象外 service/user_test.go の fakeUserRepo と同じ考え方)
func (f *fakeUserServiceRepo) checkNotLastManagerLocked(id uint64) error {
	target, ok := f.byID[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if target.Role != model.RoleManagement {
		return nil
	}
	var n int64
	for _, u := range f.byID {
		if u.Role == model.RoleManagement {
			n++
		}
	}
	if n <= 1 {
		return repository.ErrLastManagerUser
	}
	return nil
}

func (f *fakeUserServiceRepo) UpdateRoleGuarded(ctx context.Context, id uint64, role model.Role) error {
	if role == model.RoleGeneral {
		if err := f.checkNotLastManagerLocked(id); err != nil {
			return err
		}
	}
	u, ok := f.byID[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	u.Role = role
	return nil
}

func (f *fakeUserServiceRepo) Create(ctx context.Context, name, email, passwordDigest string, role model.Role, passwordExpiresAt time.Time) (*model.User, error) {
	for _, u := range f.byID {
		if u.Email == email {
			return nil, fmt.Errorf("Error 1062: Duplicate entry '%s' for key 'index_users_on_email'", email)
		}
	}
	id := uint64(len(f.byID) + 1)
	u := &model.User{ID: id, Email: email, Name: name, Role: role}
	f.byID[id] = u
	return u, nil
}

func (f *fakeUserServiceRepo) Delete(ctx context.Context, id uint64) error {
	if err := f.checkNotLastManagerLocked(id); err != nil {
		return err
	}
	delete(f.byID, id)
	return nil
}

func TestUserHandler_Provision(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("認証済みリクエストはJWTクレームからユーザーをprovisionしJSONを返す", func(t *testing.T) {
		repo := newFakeUserServiceRepo()
		h := NewUserHandler(service.NewUserService(repo))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/users/provision", nil)
		claims := &authjwt.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "sub-1"},
			Name:             "太郎",
			Email:            "taro@example.com",
		}
		authjwt.SetClaimsForTesting(c, claims)

		h.Provision(c)

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
		if body["role"] != "general" {
			t.Errorf("role = %v, want general", body["role"])
		}
	})

	t.Run("realm_access.rolesにmanagementがあればrole=managementで返す", func(t *testing.T) {
		repo := newFakeUserServiceRepo()
		h := NewUserHandler(service.NewUserService(repo))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/users/provision", nil)
		claims := &authjwt.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "admin-sub"},
			Name:             "管理者",
			Email:            "admin@example.com",
			RealmAccess:      authjwt.RealmAccess{Roles: []string{"management"}},
		}
		authjwt.SetClaimsForTesting(c, claims)

		h.Provision(c)

		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["role"] != "management" {
			t.Errorf("role = %v, want management", body["role"])
		}
	})

	t.Run("claimsが無いと401(bodyは一切参照しない)", func(t *testing.T) {
		repo := newFakeUserServiceRepo()
		h := NewUserHandler(service.NewUserService(repo))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/users/provision", nil)

		h.Provision(c)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})
}

func TestUpdateRoleRequest_Binding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"roleがあれば成功", `{"role":"management"}`, false},
		{"roleが無いと失敗", `{}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			var body updateRoleRequest
			err := c.ShouldBindJSON(&body)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestUserHandler_UpdateRole(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("最後の管理者をgeneralに変更しようとすると422", func(t *testing.T) {
		repo := newFakeUserServiceRepo()
		userService := service.NewUserService(repo)
		if _, err := userService.Provision(context.Background(), "admin-sub", "管理者", "admin@example.com", []string{"management"}); err != nil {
			t.Fatalf("Provision失敗: %v", err)
		}
		h := NewUserHandler(userService)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPatch, "/internal/v1/users/1/role", strings.NewReader(`{"role":"general"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: "1"}}

		h.UpdateRole(c)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422, body=%s", w.Code, w.Body.String())
		}
	})
}
