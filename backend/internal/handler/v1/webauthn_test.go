package v1

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// fakeWebauthnCredRepo はservice.webauthnRepositoryの最小フェイク実装
// (非公開interfaceだが、Goは構造的部分型のため異なるパッケージのfakeでも満たせる)
type fakeWebauthnCredRepo struct {
	byCredentialID map[string]*model.WebauthnCredential
	nextID         uint64
}

func newFakeWebauthnCredRepo() *fakeWebauthnCredRepo {
	return &fakeWebauthnCredRepo{byCredentialID: map[string]*model.WebauthnCredential{}}
}

func (f *fakeWebauthnCredRepo) Create(ctx context.Context, cred *model.WebauthnCredential) error {
	// 【テスト監査で追記】internal/service/webauthn_test.goのfakeWebauthnRepoと同じ理由
	// (実際のMySQLのUNIQUE制約違反エラー文言を模擬し、isDuplicateCredentialIDErrorの
	// ハンドラ層での挙動(422への変換)までテストできるようにする)
	if _, exists := f.byCredentialID[string(cred.CredentialID)]; exists {
		return errors.New("Error 1062: Duplicate entry 'x' for key 'webauthn_credentials.index_webauthn_credentials_on_credential_id'")
	}
	f.nextID++
	cred.ID = f.nextID
	f.byCredentialID[string(cred.CredentialID)] = cred
	return nil
}

func (f *fakeWebauthnCredRepo) FindByCredentialID(ctx context.Context, credentialID []byte) (*model.WebauthnCredential, error) {
	cred, ok := f.byCredentialID[string(credentialID)]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return cred, nil
}

func (f *fakeWebauthnCredRepo) UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint64) error {
	cred, ok := f.byCredentialID[string(credentialID)]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	cred.SignCount = signCount
	return nil
}

// UserIDsWithPasskey は service.passkeyChecker(UserService.WithPasskeyChecker用)も満たすための実装
// TestUserHandler_List_HasPasskeyで使う
func (f *fakeWebauthnCredRepo) UserIDsWithPasskey(ctx context.Context) (map[uint64]bool, error) {
	set := make(map[uint64]bool, len(f.byCredentialID))
	for _, cred := range f.byCredentialID {
		set[cred.UserID] = true
	}
	return set, nil
}

func newTestWebauthnHandler() *WebauthnHandler {
	return NewWebauthnHandler(service.NewWebauthnService(newFakeWebauthnCredRepo()), fakeUserLookup{user: &model.User{ID: 1}})
}

func TestWebauthnHandler_Register(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("正常な入力なら201でidを返す", func(t *testing.T) {
		h := newTestWebauthnHandler()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := `{"credential_id":"` + base64.RawURLEncoding.EncodeToString([]byte("cred-1")) + `","public_key":"` + base64.StdEncoding.EncodeToString([]byte("pubkey")) + `","sign_count":0,"transports":["internal"]}`
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/auth/webauthn/credentials", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		authjwt.SetClaimsForTesting(c, &authjwt.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "sub-1"}})

		h.Register(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201, body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal失敗: %v", err)
		}
		if resp["id"] == nil {
			t.Error("レスポンスにidが含まれていない")
		}
	})

	// 【実機バグ(CONTRACT.mdセクション22.6)の回帰テスト】
	// リクエストボディのbackup_eligible/backup_stateがservice層まで正しく渡ることを確認する
	// (ここが伝わらないと、backendには常にfalse/falseで保存され、bff側でgo-webauthnの
	// 「Backup Eligible flag inconsistency」による全ログイン失敗が再発する)
	t.Run("backup_eligible/backup_stateがtrueならservice層まで伝わる", func(t *testing.T) {
		h := newTestWebauthnHandler()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := `{"credential_id":"` + base64.RawURLEncoding.EncodeToString([]byte("cred-be")) + `","public_key":"` + base64.StdEncoding.EncodeToString([]byte("pubkey")) + `","backup_eligible":true,"backup_state":true}`
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/auth/webauthn/credentials", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		authjwt.SetClaimsForTesting(c, &authjwt.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "sub-1"}})

		h.Register(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201, body=%s", w.Code, w.Body.String())
		}
		dto, err := h.credentials.FindByCredentialID(context.Background(), []byte("cred-be"))
		if err != nil {
			t.Fatalf("FindByCredentialID() error = %v", err)
		}
		if !dto.BackupEligible || !dto.BackupState {
			t.Errorf("BackupEligible=%v BackupState=%v, want true/true", dto.BackupEligible, dto.BackupState)
		}
	})

	t.Run("未ログインなら401", func(t *testing.T) {
		h := newTestWebauthnHandler()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/auth/webauthn/credentials", strings.NewReader(`{}`))
		c.Request.Header.Set("Content-Type", "application/json")

		h.Register(c)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("credential_idがbase64urlでなければ400", func(t *testing.T) {
		h := newTestWebauthnHandler()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/auth/webauthn/credentials",
			strings.NewReader(`{"credential_id":"!!!not-base64!!!","public_key":"cGs="}`))
		c.Request.Header.Set("Content-Type", "application/json")
		authjwt.SetClaimsForTesting(c, &authjwt.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "sub-1"}})

		h.Register(c)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400, body=%s", w.Code, w.Body.String())
		}
	})

	// 【テスト監査で追加】
	// 同じ credential_id を2回登録しようとした場合に422を返すことをハンドラ層でも確認する
	//
	// service.ErrValidation が renderServiceError で422にマッピングされる経路の確認
	// 修正前はここが 500 internal_server_error になっていた
	t.Run("同じcredential_idを2回登録すると422", func(t *testing.T) {
		h := newTestWebauthnHandler()
		body := `{"credential_id":"` + base64.RawURLEncoding.EncodeToString([]byte("dup-cred")) + `","public_key":"` + base64.StdEncoding.EncodeToString([]byte("pubkey")) + `"}`

		w1 := httptest.NewRecorder()
		c1, _ := gin.CreateTestContext(w1)
		c1.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/auth/webauthn/credentials", strings.NewReader(body))
		c1.Request.Header.Set("Content-Type", "application/json")
		authjwt.SetClaimsForTesting(c1, &authjwt.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "sub-1"}})
		h.Register(c1)
		if w1.Code != http.StatusCreated {
			t.Fatalf("1回目のstatus = %d, want 201, body=%s", w1.Code, w1.Body.String())
		}

		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		c2.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/auth/webauthn/credentials", strings.NewReader(body))
		c2.Request.Header.Set("Content-Type", "application/json")
		authjwt.SetClaimsForTesting(c2, &authjwt.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "sub-1"}})
		h.Register(c2)
		if w2.Code != http.StatusUnprocessableEntity {
			t.Errorf("2回目のstatus = %d, want 422, body=%s", w2.Code, w2.Body.String())
		}
	})
}

func TestWebauthnHandler_Get(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("登録済みcredential_idなら200でuser_id等を返す", func(t *testing.T) {
		repo := newFakeWebauthnCredRepo()
		svc := service.NewWebauthnService(repo)
		_, _ = svc.Register(context.Background(), service.RegisterWebauthnCredentialInput{
			UserID: 9, CredentialID: []byte("known-cred"), PublicKey: []byte("pk"), SignCount: 3,
			BackupEligible: true, BackupState: true,
		})
		wh := NewWebauthnHandler(svc, fakeUserLookup{user: &model.User{
			ID: 9, Name: "テスト太郎", Email: "test@example.com", Role: model.RoleGeneral,
		}})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		encoded := base64.RawURLEncoding.EncodeToString([]byte("known-cred"))
		c.Request = httptest.NewRequest(http.MethodGet, "/internal/v1/auth/webauthn/credentials/"+encoded, nil)
		c.Params = gin.Params{{Key: "credential_id", Value: encoded}}

		wh.Get(c)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Unmarshal失敗: %v", err)
		}
		if resp["user_id"] != float64(9) {
			t.Errorf("user_id = %v, want 9", resp["user_id"])
		}
		// 【bffとの結合確認で追記】bffがこのレスポンスからセッションを直接発行するため、
		// name/email/rolesも含まれている必要がある
		if resp["name"] != "テスト太郎" {
			t.Errorf("name = %v, want テスト太郎", resp["name"])
		}
		if resp["email"] != "test@example.com" {
			t.Errorf("email = %v, want test@example.com", resp["email"])
		}
		roles, ok := resp["roles"].([]any)
		if !ok || len(roles) != 1 || roles[0] != "general" {
			t.Errorf("roles = %v, want [general]", resp["roles"])
		}
		// 【実機バグ(CONTRACT.mdセクション22.6)の回帰テスト】
		// bffはこの2フィールドをそのままgo-webauthnの検証用Credentialへ復元するため、
		// レスポンスに正しく含まれていないと実機でのログイン失敗が再発する
		if resp["backup_eligible"] != true {
			t.Errorf("backup_eligible = %v, want true", resp["backup_eligible"])
		}
		if resp["backup_state"] != true {
			t.Errorf("backup_state = %v, want true", resp["backup_state"])
		}
	})

	t.Run("未登録のcredential_idなら404", func(t *testing.T) {
		wh := NewWebauthnHandler(service.NewWebauthnService(newFakeWebauthnCredRepo()), fakeUserLookup{})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		encoded := base64.RawURLEncoding.EncodeToString([]byte("unknown-cred"))
		c.Request = httptest.NewRequest(http.MethodGet, "/internal/v1/auth/webauthn/credentials/"+encoded, nil)
		c.Params = gin.Params{{Key: "credential_id", Value: encoded}}

		wh.Get(c)

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404, body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("credential_idがbase64urlでなければ400", func(t *testing.T) {
		wh := NewWebauthnHandler(service.NewWebauthnService(newFakeWebauthnCredRepo()), fakeUserLookup{})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/internal/v1/auth/webauthn/credentials/!!!", nil)
		c.Params = gin.Params{{Key: "credential_id", Value: "!!!"}}

		wh.Get(c)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}

func TestWebauthnHandler_UpdateSignCount(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("登録済みなら204", func(t *testing.T) {
		repo := newFakeWebauthnCredRepo()
		svc := service.NewWebauthnService(repo)
		_, _ = svc.Register(context.Background(), service.RegisterWebauthnCredentialInput{
			UserID: 1, CredentialID: []byte("cred-sc"), PublicKey: []byte("pk"), SignCount: 1,
		})
		wh := NewWebauthnHandler(svc, fakeUserLookup{})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		encoded := base64.RawURLEncoding.EncodeToString([]byte("cred-sc"))
		c.Request = httptest.NewRequest(http.MethodPatch,
			"/internal/v1/auth/webauthn/credentials/"+encoded+"/sign-count", strings.NewReader(`{"sign_count":2}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "credential_id", Value: encoded}}

		wh.UpdateSignCount(c)
		// admin_users_test.goと同じ既知の挙動: c.Status()のみ呼ぶハンドラをrouter経由せず
		// 直接呼ぶテストパターンでは、明示的にWriteHeaderNow()しないとw.Codeが200のまま
		c.Writer.WriteHeaderNow()

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204, body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("未登録のcredential_idなら404", func(t *testing.T) {
		wh := NewWebauthnHandler(service.NewWebauthnService(newFakeWebauthnCredRepo()), fakeUserLookup{})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		encoded := base64.RawURLEncoding.EncodeToString([]byte("unknown"))
		c.Request = httptest.NewRequest(http.MethodPatch,
			"/internal/v1/auth/webauthn/credentials/"+encoded+"/sign-count", strings.NewReader(`{"sign_count":2}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "credential_id", Value: encoded}}

		wh.UpdateSignCount(c)

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404, body=%s", w.Code, w.Body.String())
		}
	})
}

// TestUserHandler_List_HasPasskey はAdmin::Usersの一覧に`has_passkey`が正しく含まれることを確認する
// (CONTRACT.mdセクション22.4・22.7)
// パスキー登録済み/未登録の両方のユーザーで確認する
func TestUserHandler_List_HasPasskey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	userRepo := newFakeUserServiceRepo()
	userService := service.NewUserService(userRepo)
	if _, err := userService.Provision(context.Background(), "sub-with-passkey", "登録済み太郎", "with-passkey@example.com", nil); err != nil {
		t.Fatalf("Provision失敗: %v", err)
	}
	if _, err := userService.Provision(context.Background(), "sub-without-passkey", "未登録花子", "without-passkey@example.com", nil); err != nil {
		t.Fatalf("Provision失敗: %v", err)
	}

	webauthnRepo := newFakeWebauthnCredRepo()
	webauthnService := service.NewWebauthnService(webauthnRepo)
	if _, err := webauthnService.Register(context.Background(), service.RegisterWebauthnCredentialInput{
		UserID: 1, CredentialID: []byte("cred-for-user-1"), PublicKey: []byte("pk"),
	}); err != nil {
		t.Fatalf("Register失敗: %v", err)
	}
	userService = userService.WithPasskeyChecker(webauthnRepo)

	h := NewUserHandler(userService)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/internal/v1/admin/users", nil)

	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Users []struct {
			ID         float64 `json:"id"`
			HasPasskey bool    `json:"has_passkey"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal失敗: %v, body=%s", err, w.Body.String())
	}
	got := map[float64]bool{}
	for _, u := range resp.Users {
		got[u.ID] = u.HasPasskey
	}
	if !got[1] {
		t.Error("user_id=1(パスキー登録済み)はhas_passkey=trueを期待")
	}
	if got[2] {
		t.Error("user_id=2(パスキー未登録)はhas_passkey=falseを期待")
	}
}

func TestRequireWebauthnInternalToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/protected", RequireWebauthnInternalToken("secret-token"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tests := []struct {
		name       string
		headerVal  string
		wantStatus int
	}{
		{name: "正しいトークンなら200", headerVal: "secret-token", wantStatus: http.StatusOK},
		{name: "トークン無しなら401", headerVal: "", wantStatus: http.StatusUnauthorized},
		{name: "誤ったトークンなら401", headerVal: "wrong-token", wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.headerVal != "" {
				req.Header.Set("X-Webauthn-Internal-Token", tt.headerVal)
			}
			router.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}
