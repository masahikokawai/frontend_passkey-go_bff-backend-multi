// Package auth はOIDC(Keycloak)とのやり取り、セッション管理、トークンリフレッシュ、
// CSRF対策など「ブラウザにはJWTを一切渡さないBFFパターン」を実現する層をまとめる
//
// Rails版はセッションに `current_user_id` を直接保存していたが(session.go参照)、
// ここではセッションに「Keycloakから取得したトークン一式」を保存し、
// ブラウザにはそのセッションを指す不透明なIDだけを渡す点が最大の違い
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
)

// Discovery はKeycloakの `/.well-known/openid-configuration` のうち
// このBFFが使うフィールドだけを取り出したもの
// oidc.Provider は標準的なフィールドしか型として持たないため、
// end_session_endpoint(RP-Initiated Logoutに必須)は Provider.Claims で
// 生JSONへ再デコードして取得する
type Discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// OIDCClient はKeycloakとのAuthorization Code + PKCEフロー一式を担当する
type OIDCClient struct {
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
	oauth2Cfg oauth2.Config
	discovery Discovery
	redis     *redis.Client
	clientID  string
}

// pendingAuth はログイン開始時にRedisへ一時保存する情報
// ブラウザ(Keycloak)を経由するリダイレクト往復の間、BFFはステートレスなので
// state/PKCE verifier/最終的な戻り先はRedisに数分だけ保持する
type pendingAuth struct {
	Verifier string `json:"verifier"`
	Redirect string `json:"redirect"`
}

const pendingAuthTTL = 5 * time.Minute

// NewOIDCClient はKeycloakのDiscoveryドキュメントを取得し、認可コードフロー用の
// クライアントを組み立てる
func NewOIDCClient(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL string, redisClient *redis.Client) (*OIDCClient, error) {
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC Discoveryの取得に失敗しました(issuer=%s): %w", issuerURL, err)
	}

	var d Discovery
	if err := provider.Claims(&d); err != nil {
		return nil, fmt.Errorf("Discoveryドキュメントの解析に失敗しました: %w", err)
	}
	if d.EndSessionEndpoint == "" {
		return nil, fmt.Errorf("Discoveryドキュメントに end_session_endpoint が含まれていません")
	}

	cfg := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  d.AuthorizationEndpoint,
			TokenURL: d.TokenEndpoint,
		},
		Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	return &OIDCClient{
		provider:  provider,
		verifier:  verifier,
		oauth2Cfg: cfg,
		discovery: d,
		redis:     redisClient,
		clientID:  clientID,
	}, nil
}

// BuildAuthURL はKeycloakのAuthorization Endpointへ飛ばすURLを生成する
// state と PKCE code_verifier を生成してRedisに一時保存し、
// コールバック時にこれらを突き合わせて検証できるようにする
func (c *OIDCClient) BuildAuthURL(ctx context.Context, redirectPath string) (string, error) {
	state, err := randomToken(32)
	if err != nil {
		return "", fmt.Errorf("stateの生成に失敗しました: %w", err)
	}
	verifier := oauth2.GenerateVerifier()

	pending := pendingAuth{Verifier: verifier, Redirect: redirectPath}
	payload, err := json.Marshal(pending)
	if err != nil {
		return "", fmt.Errorf("pendingAuthのシリアライズに失敗しました: %w", err)
	}
	if err := c.redis.Set(ctx, pendingAuthKey(state), payload, pendingAuthTTL).Err(); err != nil {
		return "", fmt.Errorf("pendingAuthの保存に失敗しました: %w", err)
	}

	authURL := c.oauth2Cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	return authURL, nil
}

// IDTokenClaims はID Tokenから取り出す最小限のクレーム
// Rails版のUser登録画面が持っていたname/emailに加え、Keycloakのrealm role
// (`realm_access.roles`)をもとにアプリの role(general/management)を決める
type IDTokenClaims struct {
	Subject string   `json:"sub"`
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	Roles   []string `json:"-"`

	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// TokenSet はKeycloakから取得したトークン一式
// RedisのSession値にそのまま格納する
type TokenSet struct {
	AccessToken     string
	RefreshToken    string
	IDToken         string
	AccessTokenExp  time.Time
	RefreshTokenExp time.Time
	Claims          IDTokenClaims
}

// ExchangeCode は `/api/auth/callback?code=...&state=...` を受けて
// 認可コードをトークンに交換し、ID Tokenを検証したうえでクレームを取り出す
// 戻り値のredirectPathはログイン開始時にBuildAuthURLへ渡されたパス
func (c *OIDCClient) ExchangeCode(ctx context.Context, code, state string) (*TokenSet, string, error) {
	raw, err := c.redis.GetDel(ctx, pendingAuthKey(state)).Result()
	if err != nil {
		return nil, "", fmt.Errorf("state %q に対応するログイン試行が見つかりません(期限切れまたはCSRFの可能性): %w", state, err)
	}
	var pending pendingAuth
	if err := json.Unmarshal([]byte(raw), &pending); err != nil {
		return nil, "", fmt.Errorf("pendingAuthの復元に失敗しました: %w", err)
	}

	token, err := c.oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(pending.Verifier))
	if err != nil {
		return nil, "", fmt.Errorf("トークン交換に失敗しました: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, "", fmt.Errorf("トークンレスポンスにid_tokenが含まれていません")
	}
	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, "", fmt.Errorf("ID Tokenの検証に失敗しました: %w", err)
	}

	var claims IDTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, "", fmt.Errorf("ID Tokenクレームの解析に失敗しました: %w", err)
	}
	claims.Roles = claims.RealmAccess.Roles

	// refresh_expires_in はOIDC標準ではなくKeycloak独自の拡張フィールド
	// golang.org/x/oauth2 の Token 構造体に無いため Extra() 経由で読む
	refreshExpiresIn, _ := token.Extra("refresh_expires_in").(float64)
	refreshExp := time.Now()
	if refreshExpiresIn > 0 {
		refreshExp = refreshExp.Add(time.Duration(refreshExpiresIn) * time.Second)
	} else {
		// Keycloakのrealm設定次第でrefresh_expires_inが返らない場合の保険
		refreshExp = refreshExp.Add(30 * time.Minute)
	}

	return &TokenSet{
		AccessToken:     token.AccessToken,
		RefreshToken:    token.RefreshToken,
		IDToken:         rawIDToken,
		AccessTokenExp:  token.Expiry,
		RefreshTokenExp: refreshExp,
		Claims:          claims,
	}, pending.Redirect, nil
}

// Refresh はrefresh_tokenを使ってAccess/Refresh/ID Tokenを更新する
// Keycloakはrefresh token rotationを行う(古いrefresh_tokenは無効化される)ため、
// 呼び出し側(session.Store経由)は必ずRedisの値を新しいTokenSetで上書きすること
func (c *OIDCClient) Refresh(ctx context.Context, refreshToken string) (*TokenSet, error) {
	src := c.oauth2Cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	token, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("リフレッシュトークンによる更新に失敗しました: %w", err)
	}

	var claims IDTokenClaims
	if rawIDToken, ok := token.Extra("id_token").(string); ok && rawIDToken != "" {
		idToken, err := c.verifier.Verify(ctx, rawIDToken)
		if err == nil {
			_ = idToken.Claims(&claims)
			claims.Roles = claims.RealmAccess.Roles
		}
	}

	refreshExpiresIn, _ := token.Extra("refresh_expires_in").(float64)
	refreshExp := time.Now()
	if refreshExpiresIn > 0 {
		refreshExp = refreshExp.Add(time.Duration(refreshExpiresIn) * time.Second)
	} else {
		refreshExp = refreshExp.Add(30 * time.Minute)
	}

	rawIDToken, _ := token.Extra("id_token").(string)

	return &TokenSet{
		AccessToken:     token.AccessToken,
		RefreshToken:    token.RefreshToken,
		IDToken:         rawIDToken,
		AccessTokenExp:  token.Expiry,
		RefreshTokenExp: refreshExp,
		Claims:          claims,
	}, nil
}

// EndSessionURL はRP-Initiated LogoutでKeycloakへリダイレクトさせるURLを組み立てる
func (c *OIDCClient) EndSessionURL(idTokenHint, postLogoutRedirectURI string) string {
	values := url.Values{}
	values.Set("id_token_hint", idTokenHint)
	values.Set("post_logout_redirect_uri", postLogoutRedirectURI)
	return c.discovery.EndSessionEndpoint + "?" + values.Encode()
}

func pendingAuthKey(state string) string {
	return "oidc_pending:" + state
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
