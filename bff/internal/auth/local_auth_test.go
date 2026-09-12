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
	"github.com/golang-jwt/jwt/v5"
)

// fakeLocalLoginClient はbackendの POST /internal/v1/auth/verify-local-password
// なしに、LoginLocal/LoginLocalRSAの分岐(成功/invalid_credentials/password_expired)を
// 検証するためのstub
type fakeLocalLoginClient struct {
	userID uint64
	name   string
	email  string
	roles  []string
	err    error
}

func (f *fakeLocalLoginClient) VerifyPassword(_ context.Context, _, _ string) (uint64, string, string, []string, error) {
	if f.err != nil {
		return 0, "", "", nil, f.err
	}
	return f.userID, f.name, f.email, f.roles, nil
}

func newTestLocalHandler(t *testing.T, localLogin LocalPasswordVerifier) (*Handler, *Store) {
	t.Helper()
	mr := newTestStore(t)
	rsaKeys, err := NewLocalRSAKeyPair()
	if err != nil {
		t.Fatalf("NewLocalRSAKeyPair() error = %v", err)
	}
	return &Handler{
		Store:                 mr,
		Flags:                 fakeFlags{},
		CookieCfg:             testCookieConfig(),
		PostLogoutRedirectURL: "http://localhost:5173/",
		FrontendBaseURL:       "http://localhost:5173",
		LocalLogin:            localLogin,
		HMACSecret:            "test-hmac-secret",
		LocalRSAKeys:          rsaKeys,
	}, mr
}

func TestHandler_LoginLocal_Success_HMAC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, store := newTestLocalHandler(t, &fakeLocalLoginClient{userID: 1, name: "ローカル太郎", email: "local-user@example.com", roles: []string{"general"}})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"email":"local-user@example.com","password":"password"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.LoginLocal(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, ck := range cookies {
		if ck.Name == "session_id" {
			sessionCookie = ck
		}
	}
	if sessionCookie == nil {
		t.Fatalf("session_id cookie was not set, cookies=%v", cookies)
	}

	sess, err := store.Get(context.Background(), sessionCookie.Value)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if sess.AuthMode != AuthModeLocalHMAC {
		t.Errorf("AuthMode = %q, want %q", sess.AuthMode, AuthModeLocalHMAC)
	}
	if sess.UserID != 1 {
		t.Errorf("UserID = %d, want 1", sess.UserID)
	}
	if sess.RefreshToken != "" {
		t.Errorf("RefreshToken = %q, want empty(ローカル認証にrefresh_tokenは存在しない)", sess.RefreshToken)
	}

	// 発行されたJWTがHS256・iss=bff-gin-local-hmac・aud=backend・sub=内部user_idであることを検証する
	claims := &LocalClaims{}
	_, err = jwt.ParseWithClaims(sess.AccessToken, claims, func(token *jwt.Token) (any, error) {
		return []byte("test-hmac-secret"), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		t.Fatalf("発行されたJWTの検証に失敗しました: %v", err)
	}
	if claims.Issuer != LocalHMACIssuer {
		t.Errorf("iss = %q, want %q", claims.Issuer, LocalHMACIssuer)
	}
	if claims.Subject != "1" {
		t.Errorf("sub = %q, want %q(内部user_id)", claims.Subject, "1")
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "backend" {
		t.Errorf("aud = %v, want [backend]", claims.Audience)
	}
}

func TestHandler_LoginLocalRSA_Success_IssuesRS256Token(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, store := newTestLocalHandler(t, &fakeLocalLoginClient{userID: 42, name: "n", email: "e", roles: []string{"general"}})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"email":"local-user@example.com","password":"password"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login/rsa", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.LoginLocalRSA(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var sessionCookie *http.Cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "session_id" {
			sessionCookie = ck
		}
	}
	if sessionCookie == nil {
		t.Fatalf("session_id cookie was not set")
	}
	sess, err := store.Get(context.Background(), sessionCookie.Value)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if sess.AuthMode != AuthModeLocalRSA {
		t.Errorf("AuthMode = %q, want %q", sess.AuthMode, AuthModeLocalRSA)
	}

	claims := &LocalClaims{}
	token, err := jwt.ParseWithClaims(sess.AccessToken, claims, func(token *jwt.Token) (any, error) {
		return h.LocalRSAKeys.PublicKey(), nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		t.Fatalf("発行されたJWTの検証に失敗しました: %v", err)
	}
	if kid, _ := token.Header["kid"].(string); kid == "" {
		t.Errorf("kidヘッダが設定されていない")
	}
	if claims.Issuer != LocalRSAIssuer {
		t.Errorf("iss = %q, want %q", claims.Issuer, LocalRSAIssuer)
	}
	if claims.Subject != "42" {
		t.Errorf("sub = %q, want %q(内部user_id)", claims.Subject, "42")
	}
}

// 【テスト監査で追加】/api/auth/login・/api/auth/login/rsaに構文が壊れたJSONを送った
// 場合の挙動が一度も確認されていなかった。ShouldBindJSONのエラーが400へマッピングされる
// ことを固定する(空文字のemail/passwordを送るテストとは違い、JSON自体が壊れているケース)
func TestHandler_LoginLocal_構文が壊れたJSONは400になりpanicしない(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestLocalHandler(t, &fakeLocalLoginClient{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{not valid json truncated`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.LoginLocal(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// emailが文字列であるべきところに配列を送る(JSONとして妥当だが型が違う)
func TestHandler_LoginLocal_フィールドの型が不一致でも400になりpanicしない(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestLocalHandler(t, &fakeLocalLoginClient{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":["a","b"],"password":"y"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.LoginLocal(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_LoginLocal_InvalidCredentials_Returns401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestLocalHandler(t, &fakeLocalLoginClient{err: ErrInvalidLocalCredentials})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"email":"x","password":"y"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.LoginLocal(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if out["error"] != "invalid_credentials" {
		t.Errorf("error = %q, want %q", out["error"], "invalid_credentials")
	}
}

func TestHandler_LoginLocal_PasswordExpired_ReturnsPasswordExpired_NotLogout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// CONTRACT.mdセクション16.3: 「ログアウトして」ではなく「そもそもログインさせない」
	// 挙動であることを検証する: セッションを作らずエラーコードだけ返すこと
	h, store := newTestLocalHandler(t, &fakeLocalLoginClient{err: ErrLocalPasswordExpired})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"email":"local-user@example.com","password":"password"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.LoginLocal(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if out["error"] != "password_expired" {
		t.Errorf("error = %q, want %q", out["error"], "password_expired")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Errorf("password_expired時にCookieが発行されている(セッションが作られてはいけない): %v", w.Result().Cookies())
	}
	_ = store
}

func TestHandler_JWKS_ReturnsRSAPublicKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newTestLocalHandler(t, &fakeLocalLoginClient{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)

	h.JWKS(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var out struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(out.Keys) != 1 {
		t.Fatalf("keys length = %d, want 1", len(out.Keys))
	}
	if out.Keys[0].Kty != "RSA" || out.Keys[0].N == "" || out.Keys[0].E == "" {
		t.Errorf("unexpected key = %+v", out.Keys[0])
	}
}

func TestHandler_Logout_LocalAuthMode_SkipsRPInitiatedLogout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, store := newTestLocalHandler(t, &fakeLocalLoginClient{})

	sessionID, err := store.Create(context.Background(), Session{
		UserID:          1,
		AuthMode:        AuthModeLocalHMAC,
		AccessToken:     "tok",
		AccessTokenExp:  time.Now().Add(time.Hour),
		RefreshTokenExp: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("store.Create() error = %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	c.Request.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})

	h.Logout(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	// ローカル認証セッションにはid_token/end_session_endpointが無いため、
	// redirectUrlはKeycloakのend_session_endpointではなくPostLogoutRedirectURLになる
	if out["redirectUrl"] != h.PostLogoutRedirectURL {
		t.Errorf("redirectUrl = %q, want %q(Keycloak end_session_endpointへは飛ばさない)", out["redirectUrl"], h.PostLogoutRedirectURL)
	}
}
