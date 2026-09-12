package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func farFuture(t *testing.T) time.Time {
	t.Helper()
	return time.Now().Add(24 * time.Hour)
}

// fakeWebauthnBackend はbackendの新規内部API(CONTRACT.mdセクション22.4)無しに、
// register/login のハンドラ分岐を検証するためのstub
type fakeWebauthnBackend struct {
	registerErr error
	// 【e2eテスト追加で追記】
	// Register に実際に渡された値を記録する
	// register→login の一連の流れを1つのテストで検証する際、
	// 登録時に go-webauthn が実際に抽出した credential_id/public_key を、
	// 続く login 時の Lookup 戻り値としてそのまま使い回すために必要(webauthn_e2e_test.go参照)
	registeredCredentialID   string
	registeredPublicKey      string
	registeredName           string
	registeredBackupEligible bool
	registeredBackupState    bool

	lookupUserID         uint64
	lookupName           string
	lookupEmail          string
	lookupRoles          []string
	lookupPublicKey      string
	lookupSignCount      uint32
	lookupBackupEligible bool
	lookupBackupState    bool
	lookupErr            error

	updateSignCountErr    error
	updateSignCountCalled bool
}

func (f *fakeWebauthnBackend) Register(_ context.Context, _ string, credentialID, publicKey string, _ uint32, backupEligible, backupState bool, _ []string, name string) error {
	f.registeredCredentialID = credentialID
	f.registeredPublicKey = publicKey
	f.registeredName = name
	f.registeredBackupEligible = backupEligible
	f.registeredBackupState = backupState
	return f.registerErr
}

func (f *fakeWebauthnBackend) Lookup(context.Context, string) (uint64, string, string, []string, string, uint32, bool, bool, error) {
	if f.lookupErr != nil {
		return 0, "", "", nil, "", 0, false, false, f.lookupErr
	}
	return f.lookupUserID, f.lookupName, f.lookupEmail, f.lookupRoles, f.lookupPublicKey, f.lookupSignCount, f.lookupBackupEligible, f.lookupBackupState, nil
}

func (f *fakeWebauthnBackend) UpdateSignCount(context.Context, string, uint32) error {
	f.updateSignCountCalled = true
	return f.updateSignCountErr
}

func newTestWebauthnHandler(t *testing.T, backend WebauthnCredentialStore) (*Handler, *Store) {
	t.Helper()
	store := newTestStore(t)
	w, err := NewWebAuthn("localhost", "bff-gin test", "http://localhost:5173")
	if err != nil {
		t.Fatalf("NewWebAuthn() error = %v", err)
	}
	return &Handler{
		Store:             store,
		CookieCfg:         testCookieConfig(),
		HMACSecret:        "test-hmac-secret",
		WebAuthn:          w,
		WebauthnChallenge: NewWebauthnChallengeStore(store.redis),
		WebauthnBackend:   backend,
	}, store
}

func createTestSessionWithMode(t *testing.T, store *Store, authMode string) string {
	t.Helper()
	sessionID, err := store.Create(context.Background(), Session{
		UserID:          1,
		Name:            "ローカル太郎",
		Email:           "local-user@example.com",
		Roles:           []string{"general"},
		AuthMode:        authMode,
		AccessToken:     "dummy-access-token",
		RefreshTokenExp: farFuture(t),
	})
	if err != nil {
		t.Fatalf("Store.Create() error = %v", err)
	}
	return sessionID
}

func TestWebauthnRegisterBegin_未ログインは401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestWebauthnHandler(t, &fakeWebauthnBackend{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/begin", nil)

	h.WebauthnRegisterBegin(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401, body=%s", w.Code, w.Body.String())
	}
}

func TestWebauthnRegisterBegin_Keycloakセッションは403(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, store := newTestWebauthnHandler(t, &fakeWebauthnBackend{})
	sessionID := createTestSessionWithMode(t, store, AuthModeKeycloak)

	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.Use(RequireSession(store, testCookieConfig()))
	r.POST("/api/auth/passkey/register/begin", h.WebauthnRegisterBegin)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/begin", nil)
	c.Request.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "webauthn_scope_local_auth_only") {
		t.Errorf("body = %s, want to contain webauthn_scope_local_auth_only", w.Body.String())
	}
}

func TestWebauthnRegisterBegin_ローカルHMACセッションは200でchallengeを返す(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, store := newTestWebauthnHandler(t, &fakeWebauthnBackend{})
	sessionID := createTestSessionWithMode(t, store, AuthModeLocalHMAC)

	w := httptest.NewRecorder()
	router := gin.New()
	router.Use(RequireSession(store, testCookieConfig()))
	router.POST("/api/auth/passkey/register/begin", h.WebauthnRegisterBegin)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/begin", nil)
	req.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("レスポンスのJSONパースに失敗: %v, body=%s", err, w.Body.String())
	}
	pk, ok := out["publicKey"].(map[string]any)
	if !ok {
		t.Fatalf("publicKeyフィールドが無い: %s", w.Body.String())
	}
	if pk["challenge"] == nil || pk["challenge"] == "" {
		t.Errorf("challengeが空: %v", pk)
	}
	authSel, ok := pk["authenticatorSelection"].(map[string]any)
	if !ok || authSel["residentKey"] != "required" {
		t.Errorf("residentKey=requiredになっていない: %v", pk["authenticatorSelection"])
	}
}

func TestWebauthnRegisterFinish_challengeが無ければ400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, store := newTestWebauthnHandler(t, &fakeWebauthnBackend{})
	sessionID := createTestSessionWithMode(t, store, AuthModeLocalHMAC)

	w := httptest.NewRecorder()
	router := gin.New()
	router.Use(RequireSession(store, testCookieConfig()))
	router.POST("/api/auth/passkey/register/finish", h.WebauthnRegisterFinish)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/finish", strings.NewReader(`{}`))
	req.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestWebauthnLoginBegin_stateとchallengeを返す(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestWebauthnHandler(t, &fakeWebauthnBackend{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/begin", nil)

	h.WebauthnLoginBegin(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("JSONパース失敗: %v", err)
	}
	if out["state"] == nil || out["state"] == "" {
		t.Errorf("stateが空: %v", out)
	}
	pk, ok := out["publicKey"].(map[string]any)
	if !ok || pk["challenge"] == nil {
		t.Errorf("publicKey.challengeが無い: %v", out)
	}
	// discoverable credential方式のため、allowCredentialsは指定しない
	if _, has := pk["allowCredentials"]; has {
		t.Errorf("allowCredentialsが設定されている(discoverable方式ではないことを期待): %v", pk)
	}
}

func TestWebauthnLoginFinish_stateが無ければ400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestWebauthnHandler(t, &fakeWebauthnBackend{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/finish", strings.NewReader(`{}`))

	h.WebauthnLoginFinish(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestWebauthnLoginFinish_未知のstateは400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestWebauthnHandler(t, &fakeWebauthnBackend{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/finish?state=does-not-exist", strings.NewReader(`{}`))

	h.WebauthnLoginFinish(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// 【テスト監査で発見・追記】これまで登録/ログイン完了エンドポイントに送るボディは
// 常に有効なJSON(`{}`または実際のattestation/assertion)のみがテストされており、
// 構文として壊れたJSON(閉じ括弧が無い等)を送った場合の挙動が一度も確認されていなかった。
// go-webauthnの内部パーサーがエラーを返すだけでpanicしないことを確認する
// (実際に手動でプローブした結果、go-webauthnはこのケースで正しくエラーを返すため、
// この結果を固定する回帰テストとして追加する)
func TestWebauthnRegisterFinish_構文が壊れたJSONは400になりpanicしない(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, store := newTestWebauthnHandler(t, &fakeWebauthnBackend{})
	sessionID := createTestSessionWithMode(t, store, AuthModeLocalHMAC)

	router := gin.New()
	router.Use(RequireSession(store, testCookieConfig()))
	router.POST("/api/auth/passkey/register/begin", h.WebauthnRegisterBegin)
	router.POST("/api/auth/passkey/register/finish", h.WebauthnRegisterFinish)

	beginReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/begin", nil)
	beginReq.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	beginRec := httptest.NewRecorder()
	router.ServeHTTP(beginRec, beginReq)
	if beginRec.Code != http.StatusOK {
		t.Fatalf("register/begin status = %d, want 200", beginRec.Code)
	}

	finishReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/finish", strings.NewReader(`{not valid json truncated`))
	finishReq.Header.Set("Content-Type", "application/json")
	finishReq.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	finishRec := httptest.NewRecorder()
	router.ServeHTTP(finishRec, finishReq)

	if finishRec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", finishRec.Code, finishRec.Body.String())
	}
}

func TestWebauthnLoginFinish_構文が壊れたJSONは400になりpanicしない(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestWebauthnHandler(t, &fakeWebauthnBackend{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/finish?state=does-not-exist", strings.NewReader(`{not valid json truncated`))

	h.WebauthnLoginFinish(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}
