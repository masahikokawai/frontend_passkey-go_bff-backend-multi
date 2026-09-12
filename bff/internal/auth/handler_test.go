package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// このファイルは実機デバッグで発覚した以下の不具合の再発を防ぐための単体テストを含む:
//   - Callback: JITプロビジョニング呼び出しにAuthorizationヘッダ(access token)が
//     渡っていなかった(fakeProvisionerで実際に渡された値を検証する)
//   - Callback: ログイン成功後のリダイレクト先が相対パスのままbff自身のオリジンへ
//     遷移してしまい404になっていた(FrontendBaseURLとの連結を検証する)
//   - Logout: レスポンスのキー名が"redirectUrl"であること(以前"logout_url"という
//     別名でfrontendと不整合を起こしていた)

type fakeProvisioner struct {
	gotAccessToken string
	gotSub         string
	gotName        string
	gotEmail       string
	gotRoles       []string
	userID         uint64
	role           string
	err            error
}

func (f *fakeProvisioner) Provision(ctx context.Context, accessToken, keycloakSub, name, email string, roles []string) (uint64, string, error) {
	f.gotAccessToken = accessToken
	f.gotSub = keycloakSub
	f.gotName = name
	f.gotEmail = email
	f.gotRoles = roles
	return f.userID, f.role, f.err
}

type fakeFlags struct{}

func (fakeFlags) BoolValue(ctx context.Context, flagKey string, defaultValue bool, userID string) bool {
	return defaultValue
}

func (fakeFlags) StringValue(ctx context.Context, flagKey string, defaultValue string, userID string) string {
	return defaultValue
}

func testCookieConfig() CookieConfig {
	return CookieConfig{SessionCookieName: "session_id", CSRFCookieName: "csrf_token", Secure: false}
}

func newTestHandler(t *testing.T, fk *fakeKeycloak, provisioner UserProvisioner) (*Handler, *Store) {
	t.Helper()
	client, redisClient := newTestOIDCClient(t, fk)
	store := NewStore(redisClient)
	return &Handler{
		OIDC:                  client,
		Store:                 store,
		Provisioner:           provisioner,
		Flags:                 fakeFlags{},
		CookieCfg:             testCookieConfig(),
		PostLogoutRedirectURL: "http://localhost:5173/",
		FrontendBaseURL:       "http://localhost:5173",
	}, store
}

func TestHandler_LoginKeycloak_RedirectsToKeycloak(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, _ := newTestHandler(t, fk, &fakeProvisioner{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/login/keycloak?redirect=/tasks", nil)

	h.LoginKeycloak(c)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, fk.server.URL) {
		t.Errorf("Location = %q, want prefix %q", loc, fk.server.URL)
	}
}

// TestIsSafeRedirectPath はOpen Redirect(CWE-601)対策の中核ロジックの単体テスト
//
// 【セキュリティ監査(3回目)で発見】`redirect`クエリパラメータは攻撃者が完全に制御できる値であり、
// 単純な文字列連結(FrontendBaseURL + redirectPath)にそのまま使うと、
// `redirect=@evil.com/phish` のような値で `http://localhost:5173@evil.com/phish` という
// URLが生成される。ブラウザはこれを「userinfo=localhost:5173, host=evil.com」と解釈するため、
// 本物のKeycloakログインを完了した直後に外部サイトへ誘導される攻撃が成立してしまう
// (実際に `python3 -c "from urllib.parse import urlparse; print(urlparse('http://localhost:5173@evil.com/phish').netloc)"`
// で `localhost:5173@evil.com` になることを確認済み)。単一の`/`で始まることを要求すれば、
// 連結後も必ず「オリジンの直後に`/`で区切られたパス」という構造になり、`@`がその前に来ることは無くなる
func TestIsSafeRedirectPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "通常のパス", path: "/tasks", want: true},
		{name: "IDを含むパス", path: "/tasks/123", want: true},
		{name: "ルート", path: "/", want: true},
		{name: "空文字", path: "", want: false},
		{name: "スラッシュ無し(パスと誤認されない)", path: "tasks", want: false},
		{name: "userinfo@トリックによるOpen Redirect", path: "@evil.com/phish", want: false},
		{name: "スキーム付き絶対URL", path: "http://evil.com", want: false},
		{name: "protocol-relative URL(//)", path: "//evil.com", want: false},
		{name: "バックスラッシュ始まり(一部ブラウザが//と同一視する)", path: "/\\evil.com", want: false},
		{name: "パス内に@が含まれてもオリジン直後でなければ安全", path: "/tasks/@evil.com", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSafeRedirectPath(tt.path); got != tt.want {
				t.Errorf("isSafeRedirectPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestHandler_LoginKeycloak_MaliciousRedirect_FallsBackToRoot は
// 公開エンドポイント(`/api/auth/login/keycloak?redirect=...`)から直接
// Open Redirectペイロードを渡された場合に、"/"へフォールバックすることを確認する
func TestHandler_LoginKeycloak_MaliciousRedirect_FallsBackToRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, _ := newTestHandler(t, fk, &fakeProvisioner{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/login/keycloak?redirect=@evil.com/phish", nil)

	h.LoginKeycloak(c)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	state := mustQueryParam(t, w.Header().Get("Location"), "state")

	// pendingAuthに保存された値が"/"にサニタイズされていること(Redisの中身を直接確認する)
	// stateキーはOIDCClient内部のprefixを使うため、ExchangeCodeを実際に呼んで戻り値で確認する
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=dummy&state="+state, nil)
	h.Callback(c2)

	loc := w2.Header().Get("Location")
	if strings.Contains(loc, "evil.com") {
		t.Fatalf("Location = %q、evil.comへのOpen Redirectが成立してしまっている", loc)
	}
	want := "http://localhost:5173/"
	if loc != want {
		t.Errorf("Location = %q, want %q(不正なredirectは/にフォールバックすべき)", loc, want)
	}
}

// TestHandler_Callback_MaliciousRedirectPath_DoesNotLeakToExternalHost は
// frontendRedirectURLの多層防御(2つ目の砦)そのものを検証する。
// LoginKeycloakの入力検証を経由せず、OIDCClient.BuildAuthURLへ直接
// 不正なredirectPathを渡した場合(=将来LoginKeycloak以外の呼び出し経路が
// 増えてもここで必ず弾かれることの確認)でも、Open Redirectが成立しないことを確認する
func TestHandler_Callback_MaliciousRedirectPath_DoesNotLeakToExternalHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, _ := newTestHandler(t, fk, &fakeProvisioner{userID: 1, role: "general"})

	// LoginKeycloakの入力検証をあえて経由せず、OIDCClient.BuildAuthURLへ
	// 直接ペイロードを渡す(isSafeRedirectPathをすり抜けた場合の最終防衛線を試す)
	authURL, err := h.OIDC.BuildAuthURL(context.Background(), "@evil.com/phish")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=dummy&state="+state, nil)

	h.Callback(c)

	loc := w.Header().Get("Location")
	if strings.Contains(loc, "evil.com") {
		t.Fatalf("Location = %q、frontendRedirectURLの多層防御をすり抜けてOpen Redirectが成立している", loc)
	}
	want := "http://localhost:5173/"
	if loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
}

// TestHandler_Callback_SessionFixation_AlwaysIssuesFreshSessionID は
// セッション固定化(Session Fixation)への耐性を確認する回帰テスト。
//
// 【セキュリティ監査(3回目)で確認、バグでは無いが未テストだった】攻撃者が被害者に
// 「あらかじめ攻撃者が知っているsession_id」を仕込んだ状態でログインさせ、ログイン後も
// そのsession_idが有効なままだと、攻撃者は自分が知っているsession_idを使って
// 被害者のログイン後セッションに成りすませてしまう(Session Fixation攻撃)。
// establishSession→Store.Createは常にcrypto/randで新しいsession_idを生成しており、
// リクエストに乗ってきた既存のCookieの値を読み取って使い回す経路が無いため、
// 実装上はこの攻撃が成立しない設計になっている。ここではそれを実際のCookie値の比較で
// 裏付ける(これまでこの性質を直接検証するテストが無かった)
func TestHandler_Callback_SessionFixation_AlwaysIssuesFreshSessionID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, _ := newTestHandler(t, fk, &fakeProvisioner{userID: 1, role: "general"})

	authURL, err := h.OIDC.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")

	// 攻撃者が被害者のブラウザへあらかじめ仕込んだ(と想定する)session_id Cookie
	const attackerChosenSessionID = "attacker-controlled-session-id-0000000000000000"

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=dummy&state="+state, nil)
	c.Request.AddCookie(&http.Cookie{Name: "session_id", Value: attackerChosenSessionID})

	h.Callback(c)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusFound, w.Body.String())
	}

	var issuedSessionID string
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "session_id" {
			issuedSessionID = ck.Value
		}
	}
	if issuedSessionID == "" {
		t.Fatal("Set-Cookie に session_id が含まれていない")
	}
	if issuedSessionID == attackerChosenSessionID {
		t.Fatalf("発行されたsession_idが攻撃者の仕込んだ値と同じ(%q)。Session Fixationが成立してしまう", issuedSessionID)
	}
}

func TestHandler_Callback_MissingCodeOrState_ReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, _ := newTestHandler(t, fk, &fakeProvisioner{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/callback", nil)

	h.Callback(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestHandler_Callback_PassesAccessTokenToProvisioner は実バグの回帰テスト:
// JITプロビジョニング呼び出しに、実際に取得したAccess Tokenが渡ること
// (以前はここでBearerトークンを一切送っておらずbackend側で401になっていた)
func TestHandler_Callback_PassesAccessTokenToProvisioner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	provisioner := &fakeProvisioner{userID: 42, role: "general"}
	h, _ := newTestHandler(t, fk, provisioner)

	authURL, err := h.OIDC.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=dummy&state="+state, nil)

	h.Callback(c)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	if provisioner.gotAccessToken != "fake-access-token" {
		t.Errorf("Provisioner.gotAccessToken = %q, want fake-access-token(空だと以前の不具合の再発)", provisioner.gotAccessToken)
	}
	if provisioner.gotSub != "sub-1" {
		t.Errorf("Provisioner.gotSub = %q, want sub-1", provisioner.gotSub)
	}
	if provisioner.gotEmail != "taro@example.com" {
		t.Errorf("Provisioner.gotEmail = %q, want taro@example.com", provisioner.gotEmail)
	}
}

// TestHandler_Callback_RedirectsToFrontendOrigin は実バグの回帰テスト:
// リダイレクト先がbff自身のオリジンではなくFrontendBaseURLになっていること
// (以前は相対パスのままc.Redirectしており、bffのオリジンへ遷移して404になっていた)
func TestHandler_Callback_RedirectsToFrontendOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, _ := newTestHandler(t, fk, &fakeProvisioner{userID: 1, role: "general"})

	authURL, err := h.OIDC.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=dummy&state="+state, nil)

	h.Callback(c)

	want := "http://localhost:5173/tasks"
	got := w.Header().Get("Location")
	if got != want {
		t.Errorf("Location = %q, want %q(bff自身のオリジンへの相対パスのままだと以前の不具合が再発する)", got, want)
	}
}

// TestHandler_Callback_SessionCookie_SecureAttributeFollowsCookieConfig は、
// テストコード監査(2回目)で発覚したテストの穴の回帰防止。
// これまでの全テストは CookieConfig.Secure を常に false に固定した testCookieConfig() を
// 使っており、「本番相当(AppEnv=production→Secure:true)でCookieに実際にSecure属性が
// 付くか」を一度も実HTTPレスポンス経由で検証していなかった(IsDevelopment()のユニットテストは
// あるが、それがCookie発行まで正しく配線されているかは別問題)。ここでは
// Secure:true/falseの両方でSet-CookieヘッダーのSecure/HttpOnly/SameSite属性を直接確認する
func TestHandler_Callback_SessionCookie_SecureAttributeFollowsCookieConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, secure := range []bool{true, false} {
		t.Run(map[bool]string{true: "Secure=true(本番相当)", false: "Secure=false(開発相当)"}[secure], func(t *testing.T) {
			fk := newFakeKeycloak(t)
			h, _ := newTestHandler(t, fk, &fakeProvisioner{userID: 1, role: "general"})
			h.CookieCfg.Secure = secure

			authURL, err := h.OIDC.BuildAuthURL(context.Background(), "/tasks")
			if err != nil {
				t.Fatalf("BuildAuthURL() error = %v", err)
			}
			state := mustQueryParam(t, authURL, "state")

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=dummy&state="+state, nil)

			h.Callback(c)

			var sessionCookie *http.Cookie
			for _, ck := range w.Result().Cookies() {
				if ck.Name == h.CookieCfg.SessionCookieName {
					sessionCookie = ck
				}
			}
			if sessionCookie == nil {
				t.Fatalf("session_idクッキーがレスポンスに含まれていない。Set-Cookie=%v", w.Header().Values("Set-Cookie"))
			}
			if sessionCookie.Secure != secure {
				t.Errorf("Secure = %v, want %v", sessionCookie.Secure, secure)
			}
			if !sessionCookie.HttpOnly {
				t.Error("HttpOnly = false, want true(セッションCookieはReactのJSから読めてはいけない)")
			}
			if sessionCookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", sessionCookie.SameSite)
			}
		})
	}
}

func TestHandler_Callback_ProvisionerError_ReturnsInternalServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	provisioner := &fakeProvisioner{err: errors.New("provision failed")}
	h, _ := newTestHandler(t, fk, provisioner)

	authURL, err := h.OIDC.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=dummy&state="+state, nil)

	h.Callback(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandler_Logout_NoCookie_ReturnsUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, _ := newTestHandler(t, fk, &fakeProvisioner{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)

	h.Logout(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestHandler_Logout_DeletesSessionAndReturnsRedirectUrlKey は、レスポンスの
// キー名が"redirectUrl"であること(以前"logout_url"という別名でfrontendと
// 不整合を起こしていた経緯があるため)を含めて確認する回帰テスト
func TestHandler_Logout_DeletesSessionAndReturnsRedirectUrlKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, store := newTestHandler(t, fk, &fakeProvisioner{})

	sessionID, err := store.Create(context.Background(), Session{
		UserID:          1,
		AuthMode:        AuthModeKeycloak,
		IDToken:         "dummy-id-token",
		RefreshTokenExp: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("store.Create() error = %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: h.CookieCfg.SessionCookieName, Value: sessionID})
	c.Request = req

	h.Logout(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスJSONのパースに失敗: %v", err)
	}
	redirectURL, ok := body["redirectUrl"]
	if !ok {
		t.Fatalf("レスポンスに redirectUrl キーが無い(logout_url等の別名になっていないか確認): %v", body)
	}
	if !strings.Contains(redirectURL, "id_token_hint=dummy-id-token") {
		t.Errorf("redirectUrl = %q, want to contain id_token_hint=dummy-id-token", redirectURL)
	}

	if _, err := store.Get(context.Background(), sessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Logout後もセッションがRedisに残っている: err=%v", err)
	}
}

func TestHandler_Me_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, _ := newTestHandler(t, fk, &fakeProvisioner{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/me", nil)

	h.Me(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestHandler_Me_Authenticated_ReturnsUserAndFlags はMeがCurrentSession(RequireSession
// ミドルウェア経由でgin.Contextに積まれた値)に依存しているため、実際にミドルウェアを
// 経由させたルーターで検証する
func TestHandler_Me_Authenticated_ReturnsUserAndFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fk := newFakeKeycloak(t)
	h, store := newTestHandler(t, fk, &fakeProvisioner{})

	sessionID, err := store.Create(context.Background(), Session{
		UserID:          7,
		Name:            "太郎",
		Email:           "taro@example.com",
		Roles:           []string{"management"},
		RefreshTokenExp: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("store.Create() error = %v", err)
	}

	router := gin.New()
	router.Use(RequireSession(store, h.CookieCfg))
	router.GET("/api/me", h.Me)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: h.CookieCfg.SessionCookieName, Value: sessionID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		User struct {
			ID    uint64 `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
		// 【CONTRACT.mdセクション19で変更】feature_flagsはbooleanのフラグ(frontend.tasks-ts-rewrite等)と
		// 文字列のフラグ(frontend.task-create-ux)が混在するため、map[string]boolでは
		// json.Unmarshal自体が失敗する。map[string]anyで受ける
		FeatureFlags map[string]any `json:"feature_flags"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスJSONのパースに失敗: %v", err)
	}
	if body.User.ID != 7 {
		t.Errorf("User.ID = %d, want 7", body.User.ID)
	}
	if body.User.Name != "太郎" {
		t.Errorf("User.Name = %q, want 太郎", body.User.Name)
	}
	if body.User.Role != "management" {
		t.Errorf("User.Role = %q, want management", body.User.Role)
	}
	if _, ok := body.FeatureFlags["frontend.tasks-ts-rewrite"]; !ok {
		t.Errorf("feature_flags に frontend.tasks-ts-rewrite が含まれていない: %v", body.FeatureFlags)
	}
	if v, ok := body.FeatureFlags["frontend.task-create-ux"]; !ok {
		t.Errorf("feature_flags に frontend.task-create-ux が含まれていない: %v", body.FeatureFlags)
	} else if v != "inline" {
		// fakeFlags.StringValueはdefaultValueをそのまま返すので、Me()が渡す既定値"inline"のはず
		t.Errorf("feature_flags[frontend.task-create-ux] = %v, want \"inline\"", v)
	}
}
