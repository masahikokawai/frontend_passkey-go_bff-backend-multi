package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/redis/go-redis/v9"
)

// このファイルは実機デバッグで発覚した以下の不具合の再発を防ぐための単体テストを含む:
//   - Keycloakの認可コードフローで発行されるAccess Tokenにaudクレームが無く、
//     backend側のJWT検証が常に失敗していた(ID Token検証自体は別レイヤーだが、
//     ここでは「署名・audienceが正しく検証されているか」を確認する)
//   - Discovery/JWKS/トークンエンドポイントを本物のKeycloak無しに検証できるよう、
//     httptest.Serverでフェイクし、RSA鍵ペアもテスト内で自己完結して生成する

const testClientID = "bff-gin"

type fakeKeycloak struct {
	server     *httptest.Server
	privateKey *rsa.PrivateKey
	kid        string
	// tokenOverride: 各テストがトークンエンドポイントの応答を差し替えたい場合に使う
	// nilなら defaultClaims() を正しい鍵で署名した通常成功レスポンスを返す
	tokenOverride func(w http.ResponseWriter, r *http.Request)
}

func newFakeKeycloak(t *testing.T) *fakeKeycloak {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	fk := &fakeKeycloak{privateKey: priv, kid: "test-key-1"}

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	fk.server = server
	t.Cleanup(server.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/auth",
			"token_endpoint":         server.URL + "/token",
			"end_session_endpoint":   server.URL + "/logout",
			"jwks_uri":               server.URL + "/certs",
		})
	})
	mux.HandleFunc("/certs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		jwk := jose.JSONWebKey{Key: &fk.privateKey.PublicKey, KeyID: fk.kid, Algorithm: "RS256", Use: "sig"}
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if fk.tokenOverride != nil {
			fk.tokenOverride(w, r)
			return
		}
		fk.writeTokenResponse(t, w, fk.signIDToken(t, fk.defaultClaims()))
	})

	return fk
}

func (fk *fakeKeycloak) defaultClaims() map[string]any {
	return map[string]any{
		"iss":   fk.server.URL,
		"aud":   testClientID,
		"sub":   "sub-1",
		"name":  "太郎",
		"email": "taro@example.com",
		"exp":   nowUnixPlusHour(),
		"iat":   nowUnix(),
		"realm_access": map[string]any{
			"roles": []string{"general"},
		},
	}
}

// signIDToken は正しい(JWKSに公開鍵が載っている)鍵で署名する
func (fk *fakeKeycloak) signIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	return signClaims(t, fk.privateKey, fk.kid, claims)
}

// signIDTokenWithWrongKey は別のRSA鍵(JWKSの公開鍵とは無関係)で署名する
// 改ざん・なりすましトークンを模し、署名検証が本当に機能しているかを確認するために使う
func (fk *fakeKeycloak) signIDTokenWithWrongKey(t *testing.T, claims map[string]any) string {
	t.Helper()
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	return signClaims(t, wrongKey, fk.kid, claims)
}

func signClaims(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), kid),
	)
	if err != nil {
		t.Fatalf("jose.NewSigner() error = %v", err)
	}
	token, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("id_tokenの署名に失敗しました: %v", err)
	}
	return token
}

func nowUnix() int64 {
	return time.Now().Unix()
}

func nowUnixPlusHour() int64 {
	return time.Now().Add(time.Hour).Unix()
}

func (fk *fakeKeycloak) writeTokenResponse(t *testing.T, w http.ResponseWriter, idToken string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"access_token":       "fake-access-token",
		"refresh_token":      "fake-refresh-token",
		"token_type":         "Bearer",
		"expires_in":         300,
		"refresh_expires_in": 1800,
	}
	if idToken != "" {
		resp["id_token"] = idToken
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// newTestOIDCClient はfakeKeycloakを指すOIDCClientと、テスト側からも検証できるように
// 同じRedisクライアントを返す
func newTestOIDCClient(t *testing.T, fk *fakeKeycloak) (*OIDCClient, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { redisClient.Close() })

	client, err := NewOIDCClient(context.Background(), fk.server.URL, testClientID, "test-secret", "http://localhost:8080/api/auth/callback", redisClient)
	if err != nil {
		t.Fatalf("NewOIDCClient() error = %v", err)
	}
	return client, redisClient
}

func TestBuildAuthURL_ContainsRequiredParams(t *testing.T) {
	fk := newFakeKeycloak(t)
	client, _ := newTestOIDCClient(t, fk)

	authURL, err := client.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	q := parsed.Query()

	if got := q.Get("client_id"); got != testClientID {
		t.Errorf("client_id = %q, want %q", got, testClientID)
	}
	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want %q", got, "code")
	}
	if got := q.Get("redirect_uri"); got != "http://localhost:8080/api/auth/callback" {
		t.Errorf("redirect_uri = %q, want %q", got, "http://localhost:8080/api/auth/callback")
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge is empty")
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if q.Get("state") == "" {
		t.Error("state is empty")
	}
	if !strings.Contains(q.Get("scope"), "openid") {
		t.Errorf("scope = %q, want to contain openid", q.Get("scope"))
	}
}

// TestBuildAuthURL_CodeChallengeMatchesS256OfVerifier はPKCEのcode_challengeが
// 「Redisに保存したcode_verifierをS256でハッシュした値」と一致することを確認する
func TestBuildAuthURL_CodeChallengeMatchesS256OfVerifier(t *testing.T) {
	fk := newFakeKeycloak(t)
	client, redisClient := newTestOIDCClient(t, fk)

	authURL, err := client.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")
	challenge := mustQueryParam(t, authURL, "code_challenge")

	raw, err := redisClient.Get(context.Background(), pendingAuthKey(state)).Result()
	if err != nil {
		t.Fatalf("Redisからpending authを取得できない: %v", err)
	}
	var pending pendingAuth
	if err := json.Unmarshal([]byte(raw), &pending); err != nil {
		t.Fatalf("pendingAuthのUnmarshalに失敗: %v", err)
	}

	sum := sha256.Sum256([]byte(pending.Verifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != wantChallenge {
		t.Errorf("code_challenge = %q, want %q (保存したverifierのS256)", challenge, wantChallenge)
	}
}

func TestExchangeCode_Success(t *testing.T) {
	fk := newFakeKeycloak(t)
	client, _ := newTestOIDCClient(t, fk)

	authURL, err := client.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")

	tokens, redirect, err := client.ExchangeCode(context.Background(), "dummy-code", state)
	if err != nil {
		t.Fatalf("ExchangeCode() error = %v", err)
	}
	if redirect != "/tasks" {
		t.Errorf("redirect = %q, want /tasks", redirect)
	}
	if tokens.AccessToken != "fake-access-token" {
		t.Errorf("AccessToken = %q, want fake-access-token", tokens.AccessToken)
	}
	if tokens.Claims.Subject != "sub-1" {
		t.Errorf("Claims.Subject = %q, want sub-1", tokens.Claims.Subject)
	}
	if tokens.Claims.Email != "taro@example.com" {
		t.Errorf("Claims.Email = %q, want taro@example.com", tokens.Claims.Email)
	}
	if len(tokens.Claims.Roles) != 1 || tokens.Claims.Roles[0] != "general" {
		t.Errorf("Claims.Roles = %v, want [general]", tokens.Claims.Roles)
	}
}

func TestExchangeCode_UnknownState_ReturnsError(t *testing.T) {
	fk := newFakeKeycloak(t)
	client, _ := newTestOIDCClient(t, fk)

	_, _, err := client.ExchangeCode(context.Background(), "dummy-code", "never-issued-state")
	if err == nil {
		t.Fatal("ExchangeCode() with unknown state should return an error")
	}
}

func TestExchangeCode_MissingIDToken_ReturnsError(t *testing.T) {
	fk := newFakeKeycloak(t)
	fk.tokenOverride = func(w http.ResponseWriter, r *http.Request) {
		fk.writeTokenResponse(t, w, "")
	}
	client, _ := newTestOIDCClient(t, fk)

	authURL, err := client.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")

	_, _, err = client.ExchangeCode(context.Background(), "dummy-code", state)
	if err == nil {
		t.Fatal("id_tokenが無いレスポンスはエラーになるべき")
	}
}

// TestExchangeCode_IDTokenSignedWithWrongKey_ReturnsError はJWKSの鍵と一致しない
// 署名のID Tokenを拒否できることを確認する(署名検証が実際に機能していることの確認)
func TestExchangeCode_IDTokenSignedWithWrongKey_ReturnsError(t *testing.T) {
	fk := newFakeKeycloak(t)
	fk.tokenOverride = func(w http.ResponseWriter, r *http.Request) {
		idToken := fk.signIDTokenWithWrongKey(t, fk.defaultClaims())
		fk.writeTokenResponse(t, w, idToken)
	}
	client, _ := newTestOIDCClient(t, fk)

	authURL, err := client.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")

	_, _, err = client.ExchangeCode(context.Background(), "dummy-code", state)
	if err == nil {
		t.Fatal("JWKSの鍵と一致しない署名のID Tokenは拒否されるべき")
	}
}

// TestExchangeCode_IDTokenWrongAudience_ReturnsError は、実機で見つかった
// 「audクレームの不一致で検証が通らない」系の不具合と同じ観点(audience検証)を
// ID Token側で確認する回帰テスト
func TestExchangeCode_IDTokenWrongAudience_ReturnsError(t *testing.T) {
	fk := newFakeKeycloak(t)
	fk.tokenOverride = func(w http.ResponseWriter, r *http.Request) {
		claims := fk.defaultClaims()
		claims["aud"] = "some-other-client"
		idToken := fk.signIDToken(t, claims)
		fk.writeTokenResponse(t, w, idToken)
	}
	client, _ := newTestOIDCClient(t, fk)

	authURL, err := client.BuildAuthURL(context.Background(), "/tasks")
	if err != nil {
		t.Fatalf("BuildAuthURL() error = %v", err)
	}
	state := mustQueryParam(t, authURL, "state")

	_, _, err = client.ExchangeCode(context.Background(), "dummy-code", state)
	if err == nil {
		t.Fatal("audienceが一致しないID Tokenは拒否されるべき")
	}
}

// 【テスト監査で追加】Keycloakはrefresh token rotationを行いうる(古いrefresh_tokenを
// 無効化し、リフレッシュのたびに新しいrefresh_tokenを発行する)。多くのOAuthクライアント
// 実装がaccess_tokenだけ見てrefresh_tokenの更新を見落とすバグを埋め込みがちなクラスの
// 不具合のため、実際にレスポンスのrefresh_tokenが要求時と異なる値でも、
// OIDCClient.Refresh()が新しい値を正しく返す(呼び出し元がRedisへ保存する値)ことを確認する
func TestRefresh_RotatedRefreshTokenIsReturned(t *testing.T) {
	fk := newFakeKeycloak(t)
	fk.tokenOverride = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":       "rotated-access-token",
			"refresh_token":      "rotated-refresh-token",
			"token_type":         "Bearer",
			"expires_in":         300,
			"refresh_expires_in": 1800,
		})
	}
	client, _ := newTestOIDCClient(t, fk)

	tokens, err := client.Refresh(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if tokens.AccessToken != "rotated-access-token" {
		t.Errorf("AccessToken = %q, want rotated-access-token", tokens.AccessToken)
	}
	if tokens.RefreshToken != "rotated-refresh-token" {
		t.Errorf("RefreshToken = %q, want rotated-refresh-token(古いrefresh_tokenのまま上書きされていないか、"+
			"新しい値を無視していないかを確認する)", tokens.RefreshToken)
	}
}

// 【テスト監査で追加】Keycloakのrealm設定次第では、リフレッシュのレスポンスに
// refresh_tokenフィールド自体が含まれないことがある(rotation無効時など)。この場合、
// 依存ライブラリ(golang.org/x/oauth2)が古いrefresh_tokenを保持するのか、空文字列で
// 上書きしてしまうのかは自明ではないため、実際の挙動を固定する(bff-railsの
// Auth::TokenRefresherは同じケースに対し明示的なフォールバック(body.fetch(...,
// refresh_token))を持つが、bffはgolang.org/x/oauth2任せになっているため、その依存先の
// 挙動が変わった場合に検知できるようにする)
func TestRefresh_ResponseOmitsRefreshToken(t *testing.T) {
	fk := newFakeKeycloak(t)
	fk.tokenOverride = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":       "new-access-token",
			"token_type":         "Bearer",
			"expires_in":         300,
			"refresh_expires_in": 1800,
		})
	}
	client, _ := newTestOIDCClient(t, fk)

	tokens, err := client.Refresh(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	// 【現状の実際の挙動を記録するテスト】golang.org/x/oauth2はrefresh_tokenが
	// レスポンスに無い場合、TokenSourceに渡した古いrefresh_tokenをToken().RefreshTokenに
	// 引き継ぐ(内部のtokenRefresher.Tokenが要求時の値を保持したまま返す挙動に依存している)。
	// この結果、呼び出し元(Refresher.Do)が保存する値も古いrefresh_tokenのまま維持される
	if tokens.RefreshToken != "old-refresh-token" {
		t.Errorf("RefreshToken = %q, want old-refresh-token(レスポンスにrefresh_tokenが"+
			"無い場合は古い値を維持するはずだが、空文字列等で上書きされている場合は要注意)", tokens.RefreshToken)
	}
}

func TestEndSessionURL_ContainsIDTokenHintAndRedirect(t *testing.T) {
	fk := newFakeKeycloak(t)
	client, _ := newTestOIDCClient(t, fk)

	got := client.EndSessionURL("dummy-id-token", "http://localhost:5173/")
	if !strings.HasPrefix(got, fk.server.URL+"/logout") {
		t.Errorf("EndSessionURL = %q, want prefix %q", got, fk.server.URL+"/logout")
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	q := parsed.Query()
	if q.Get("id_token_hint") != "dummy-id-token" {
		t.Errorf("id_token_hint = %q, want dummy-id-token", q.Get("id_token_hint"))
	}
	if q.Get("post_logout_redirect_uri") != "http://localhost:5173/" {
		t.Errorf("post_logout_redirect_uri = %q, want http://localhost:5173/", q.Get("post_logout_redirect_uri"))
	}
}

func mustQueryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", rawURL, err)
	}
	return parsed.Query().Get(key)
}
