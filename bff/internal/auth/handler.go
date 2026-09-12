package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/webauthn"
)

// UserProvisioner はbackendへJIT(Just-In-Time)プロビジョニングを依頼する
// CONTRACT.mdセクション10: Keycloakでの認証成功後、ID Tokenのsub/name/email/roles
// を使ってアプリ自身のusersテーブルへupsertする。Rails版のresources :users
// (サインアップ画面)に相当する処理が不要になる代わりに、この一手間が必要になる
type UserProvisioner interface {
	// accessToken: backendの /internal/v1/users/provision は他の全エンドポイントと同じく
	// RequireAuthミドルウェア配下にあり、有効なBearerトークンを要求する
	// (統合時に発覚: 当初トークンを送らずネットワーク分離のみに頼る設計になっていたが、
	// backend側は一貫してJWT検証済みclaimsからしか身元を信頼しない設計のため、ここも合わせる)
	Provision(ctx context.Context, accessToken, keycloakSub, name, email string, roles []string) (userID uint64, role string, err error)
}

// FlagEvaluator は /api/me が返す feature_flags を評価するための最小interface
// 実体はinternal/featureflag.Evaluatorだが、authパッケージがfeatureflagパッケージへ
// 依存しないようにここでは構造的部分型として宣言するだけに留める
type FlagEvaluator interface {
	BoolValue(ctx context.Context, flagKey string, defaultValue bool, userID string) bool
	// StringValue はCONTRACT.mdセクション19: frontend.task-create-uxのような、
	// 排他的な複数の選択肢から1つを選ぶ多値(multivariate)フラグを評価するためのメソッド
	StringValue(ctx context.Context, flagKey string, defaultValue string, userID string) string
}

// LocalPasswordVerifier はbackendへのパスワード照会を抽象化する最小interface
// 実体は*LocalLoginClientだが、単体テストでは実際のbackendなしに
// 成功/invalid_credentials/password_expiredの各分岐を検証したいため抽象化する
type LocalPasswordVerifier interface {
	VerifyPassword(ctx context.Context, email, password string) (userID uint64, name, userEmail string, roles []string, err error)
}

// Handler はOIDC関連のHTTPエンドポイント一式
type Handler struct {
	OIDC                  *OIDCClient
	Store                 *Store
	Provisioner           UserProvisioner
	Flags                 FlagEvaluator
	CookieCfg             CookieConfig
	PostLogoutRedirectURL string
	// FrontendBaseURL はログイン成功後のリダイレクト先のオリジン(config.FrontendBaseURL参照)
	FrontendBaseURL string

	// 以下3つはCONTRACT.mdセクション16(ローカル認証)で追加
	LocalLogin   LocalPasswordVerifier
	HMACSecret   string
	LocalRSAKeys *LocalRSAKeyPair

	// 以下はCONTRACT.mdセクション22(パスキー)で追加
	WebAuthn          *webauthn.WebAuthn
	WebauthnChallenge *WebauthnChallengeStore
	WebauthnBackend   WebauthnCredentialStore

	// Logger はnilでも動作する(未設定時はログ出力しない)。
	// 【実機デバッグで追記】go-webauthnがFinishPasskeyLogin等で返すエラーは、
	// これまでクライアントへ汎用的なJSON({"error":"webauthn_verification_failed"}等)を
	// 返すだけでbff側のログに一切残っておらず、実機での原因調査が困難だった。
	// TaskRoutes/LabelRoutesと同じくLoggerを持たせ、検証失敗時は必ず理由を残す
	Logger *slog.Logger
}

// isSafeRedirectPath は `redirect` クエリパラメータがOpen Redirectに悪用されない
// 「同一オリジンへの相対パス」であることを確認する
//
// 【3回目のテスト監査で発見・修正、重要】Callbackは検証済みredirectPathを
// `strings.TrimSuffix(h.FrontendBaseURL, "/")+redirectPath` という単純な文字列結合で
// Location ヘッダに使っている。ここでredirectPathが `@evil.com/phish` のような値だと、
// 結合結果は `http://localhost:5173@evil.com/phish` になり、ブラウザのURL解析では
// `localhost:5173` がuserinfo、`evil.com` が実際のhostとして扱われる
// (`python3 -c "from urllib.parse import urlparse; print(urlparse('http://localhost:5173@evil.com/phish'))"`
// で `netloc='localhost:5173@evil.com'` になることを実際に確認済み)。
// つまり「本物のKeycloakログインを完了した直後に外部ドメインへ誘導される」
// 攻撃的なOpen Redirectが成立してしまう。redirectPathの先頭が単一の `/` であることを
// 強制することで、後続の文字列結合が常に既存オリジン配下のパスとしてのみ解釈されるようにする
// (`//evil.com` のようなprotocol-relative URLも、単一`/`ではなく`//`始まりのため弾かれる)
func isSafeRedirectPath(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	if strings.HasPrefix(path, "//") || strings.HasPrefix(path, "/\\") {
		return false
	}
	return true
}

// frontendRedirectURL はCallbackがfrontendへ302する際の絶対URLを組み立てる
// redirectPathはOIDCログイン開始時(LoginKeycloak)の時点でisSafeRedirectPathを
// 満たすよう検証済みのはずだが、Redis(pendingAuth)を経由して戻ってくる値を
// 無条件に信頼せず、ここでも同じ検証をもう一度行う(多層防御。将来
// BuildAuthURLを別の未検証な入力から呼ぶ経路が増えても安全側に倒れるようにする)
func (h *Handler) frontendRedirectURL(redirectPath string) string {
	if !isSafeRedirectPath(redirectPath) {
		redirectPath = "/"
	}
	return strings.TrimSuffix(h.FrontendBaseURL, "/") + redirectPath
}

// LoginKeycloak は `GET /api/auth/login/keycloak?redirect=<path>` 。Keycloakへ302する
// 【CONTRACT.mdセクション16.1】以前は `GET /api/auth/login` だったが、ローカル認証
// (2方式)を追加したことで「ログインURLを3つに分ける」設計に変わったためリネームした
func (h *Handler) LoginKeycloak(c *gin.Context) {
	redirect := c.DefaultQuery("redirect", "/")
	if !isSafeRedirectPath(redirect) {
		// Open Redirect対策(isSafeRedirectPathのコメント参照): 不正な値は既定の"/"にフォールバックする
		// (エラーにはしない。ここはユーザーが直接踏む可能性のあるリンクの入口であり、
		// 攻撃者が細工したリンクを踏んでも安全な場所へ誘導されるだけにするのが望ましいため)
		redirect = "/"
	}
	authURL, err := h.OIDC.BuildAuthURL(c.Request.Context(), redirect)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, authURL)
}

// establishSession はCookie発行+セッション作成の共通処理
// Keycloakログイン(Callback)・ローカルログイン(LoginLocal/LoginLocalRSA)の
// いずれもこの処理を経る(CONTRACT.mdセクション16.4)
func (h *Handler) establishSession(c *gin.Context, sess Session) error {
	sessionID, err := h.Store.Create(c.Request.Context(), sess)
	if err != nil {
		return err
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.CookieCfg.SessionCookieName, sessionID, 0, "/", "", h.CookieCfg.Secure, true)
	if _, err := IssueCSRFCookie(c, h.CookieCfg); err != nil {
		return err
	}
	return nil
}

type localLoginRequestBody struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginLocal は `POST /api/auth/login`。ローカル認証(HMAC版、既定)
// CONTRACT.mdセクション16.3/16.4: backendへパスワード照合を委譲し、成功したら
// bff自身がHS256署名のJWTを発行してセッションを確立する
func (h *Handler) LoginLocal(c *gin.Context) {
	h.loginLocal(c, AuthModeLocalHMAC)
}

// LoginLocalRSA は `POST /api/auth/login/rsa`。ローカル認証(RSA版)
func (h *Handler) LoginLocalRSA(c *gin.Context) {
	h.loginLocal(c, AuthModeLocalRSA)
}

func (h *Handler) loginLocal(c *gin.Context, authMode string) {
	var req localLoginRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email/password is required"})
		return
	}

	ctx := c.Request.Context()
	userID, name, email, roles, err := h.LocalLogin.VerifyPassword(ctx, req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrLocalPasswordExpired):
			// CONTRACT.mdセクション16.3: 「期限切れ→自動ログアウト」ではなく
			// 「そもそもログインさせない」。セッションは作らずここで終わる
			c.JSON(http.StatusUnauthorized, gin.H{"error": "password_expired"})
		case errors.Is(err, ErrInvalidLocalCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	var (
		tokenString string
		exp         time.Time
	)
	switch authMode {
	case AuthModeLocalRSA:
		tokenString, exp, err = h.LocalRSAKeys.IssueToken(userID, name, email, roles)
	default:
		tokenString, exp, err = IssueLocalHMACToken(h.HMACSecret, userID, name, email, roles)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// ローカルセッションにはrefresh_tokenが存在しない(CONTRACT.mdセクション16.4)
	// Store.saveのTTLガード(RefreshTokenExpが未来であること)を満たすため、
	// AccessTokenExpと同じ値を入れておく(実際にリフレッシュには使われない)
	sess := Session{
		UserID:          userID,
		Name:            name,
		Email:           email,
		Roles:           roles,
		AuthMode:        authMode,
		AccessToken:     tokenString,
		RefreshToken:    "",
		AccessTokenExp:  exp,
		RefreshTokenExp: exp,
	}
	if err := h.establishSession(c, sess); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"redirect": "/"})
}

// Callback は `GET /api/auth/callback?code=&state=`
// トークン交換→JITプロビジョニング→セッション発行→Cookie設定→元のredirect先へ302
func (h *Handler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code/state is required"})
		return
	}

	ctx := c.Request.Context()
	tokens, redirectPath, err := h.OIDC.ExchangeCode(ctx, code, state)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("login failed: %v", err)})
		return
	}

	userID, role, err := h.Provisioner.Provision(ctx, tokens.AccessToken, tokens.Claims.Subject, tokens.Claims.Name, tokens.Claims.Email, tokens.Claims.Roles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("user provisioning failed: %v", err)})
		return
	}
	roles := tokens.Claims.Roles
	if role != "" {
		roles = append(roles, role)
	}

	sess := Session{
		UserID:          userID,
		KeycloakSub:     tokens.Claims.Subject,
		Name:            tokens.Claims.Name,
		Email:           tokens.Claims.Email,
		Roles:           roles,
		AuthMode:        AuthModeKeycloak,
		AccessToken:     tokens.AccessToken,
		RefreshToken:    tokens.RefreshToken,
		IDToken:         tokens.IDToken,
		AccessTokenExp:  tokens.AccessTokenExp,
		RefreshTokenExp: tokens.RefreshTokenExp,
	}
	if err := h.establishSession(c, sess); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// redirectPathはfrontendが渡した相対パス(例: "/tasks")でしかないため、
	// bff自身のオリジン(:8080)ではなくfrontendのオリジンへ絶対URLで
	// リダイレクトする必要がある(統合検証で判明: これをせず相対パスのまま
	// c.Redirectすると、ブラウザはbffのオリジンからの相対パスとして解釈し
	// http://localhost:8080/tasks へ遷移してbffのGinルーターに一致するルートが無く
	// 「404 page not found」になっていた)
	//
	// 【セキュリティ監査(3回目)で発覚・修正】以前はここを単純な文字列連結
	// (FrontendBaseURL + redirectPath)にしていたが、redirectPathは
	// `/api/auth/login/keycloak?redirect=...` のクエリパラメータ経由で
	// 攻撃者が完全に制御できる値であり、例えば `redirect=@evil.com/phish` を渡すと
	// 連結結果が `http://localhost:5173@evil.com/phish` となり、ブラウザは
	// `localhost:5173` をuserinfo、`evil.com` をhostとして解釈してしまう
	// (Open Redirect、CWE-601)。正規のKeycloakログインを経由させた上で
	// 任意の外部サイトへ転送できてしまうため、frontendRedirectURLで
	// 必ず自オリジンのパスに限定してから使う
	c.Redirect(http.StatusFound, h.frontendRedirectURL(redirectPath))
}

// Logout は `POST /api/auth/logout`。RP-Initiated Logoutのみ実装(CONTRACT.md参照)
func (h *Handler) Logout(c *gin.Context) {
	sessionID, err := c.Cookie(h.CookieCfg.SessionCookieName)
	if err != nil || sessionID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	ctx := c.Request.Context()
	sess, err := h.Store.Get(ctx, sessionID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	idToken := sess.IDToken
	authMode := sess.AuthMode

	if err := h.Store.Delete(ctx, sessionID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.SetCookie(h.CookieCfg.SessionCookieName, "", -1, "/", "", h.CookieCfg.Secure, true)
	c.SetCookie(h.CookieCfg.CSRFCookieName, "", -1, "/", "", h.CookieCfg.Secure, false)

	// CONTRACT.mdセクション16.4: ローカル認証(refresh_token/id_tokenを持たない)には
	// RP-Initiated Logoutの概念が無いため、Keycloakセッションの場合のみ
	// end_session_endpointへのリダイレクトURLを返す。それ以外はセッション/Cookie削除のみで
	// 完了し、frontendのトップ(=ログイン画面)へ戻す
	//
	// キー名はfrontendのLayout.tsx(LogoutResponse.redirectUrl)に合わせる(統合時に発覚した不整合)
	if authMode == AuthModeKeycloak {
		endSessionURL := h.OIDC.EndSessionURL(idToken, h.PostLogoutRedirectURL)
		c.JSON(http.StatusOK, gin.H{"redirectUrl": endSessionURL})
		return
	}
	c.JSON(http.StatusOK, gin.H{"redirectUrl": h.PostLogoutRedirectURL})
}

// JWKS は `GET /.well-known/jwks.json`。ローカル認証(RSA版)の検証用に
// bffが自分のRSA公開鍵を配布する(CONTRACT.mdセクション16.4)。認証不要
func (h *Handler) JWKS(c *gin.Context) {
	c.JSON(http.StatusOK, h.LocalRSAKeys.JWKS())
}

// Me は `GET /api/me`。ログイン状態確認とFeature Flagの配信を兼ねる
func (h *Handler) Me(c *gin.Context) {
	sess, _, ok := CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	if _, err := IssueCSRFCookie(c, h.CookieCfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	role := "general"
	for _, r := range sess.Roles {
		if r == "management" {
			role = "management"
		}
	}

	userIDStr := fmt.Sprintf("%d", sess.UserID)
	flags := gin.H{
		"frontend.tasks-ts-rewrite": h.Flags.BoolValue(c.Request.Context(), "frontend.tasks-ts-rewrite", false, userIDStr),
		// CONTRACT.mdセクション19: Task登録UXの3パターン(inline/modal/page)を切り替える
		// 多値フラグ。他のbooleanフラグと同じ`feature_flags`オブジェクトに混在させるため、
		// このレスポンス全体を厳密な型(map[string]bool等)で受けるコードは無いことが前提
		// (frontend側はこのオブジェクトを`Record<string, boolean | string>`相当で扱う)
		"frontend.task-create-ux": h.Flags.StringValue(c.Request.Context(), "frontend.task-create-ux", "inline", userIDStr),
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":    sess.UserID,
			"name":  sess.Name,
			"email": sess.Email,
			"role":  role,
		},
		"feature_flags": flags,
	})
}
