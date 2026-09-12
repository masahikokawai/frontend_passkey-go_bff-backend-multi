package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// contextSessionKey はgin.ContextにSessionを格納する際のキー
const contextSessionKey = "auth.session"
const contextSessionIDKey = "auth.session_id"

// CookieName / IsDevelopmentは main.go からCookie発行時にも使う設定値
type CookieConfig struct {
	SessionCookieName string
	CSRFCookieName    string
	Secure            bool
}

// RequireSession はCookieからsession_idを取り出し、Redisのセッションを検証する
// 未ログイン(Cookieなし/セッション切れ)なら401を返してハンドラチェーンを止める
// Rails版のApplicationController#require_session(session[:current_user_id]を見る)
// に相当するが、ここでは「トークンの入れ物」を見ている点が異なる
func RequireSession(store *Store, cfg CookieConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, err := c.Cookie(cfg.SessionCookieName)
		if err != nil || sessionID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
			return
		}

		sess, err := store.Get(c.Request.Context(), sessionID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
			return
		}

		c.Set(contextSessionKey, sess)
		c.Set(contextSessionIDKey, sessionID)
		c.Next()
	}
}

// CurrentSession はハンドラ内で RequireSession 通過後のセッションを取り出す
func CurrentSession(c *gin.Context) (*Session, string, bool) {
	sessRaw, ok := c.Get(contextSessionKey)
	if !ok {
		return nil, "", false
	}
	idRaw, ok := c.Get(contextSessionIDKey)
	if !ok {
		return nil, "", false
	}
	return sessRaw.(*Session), idRaw.(string), true
}

// ErrUpstreamUnauthorized はbackend呼び出し関数(Refresher.Doに渡すfn)が
// 「access tokenの期限切れによる401だった」ことを示すために返すセンチネルエラー
// backend固有のエラー型に依存させないための抽象化
var ErrUpstreamUnauthorized = errors.New("upstream returned unauthorized")

// ErrUserNotProvisioned は backend が
// 「JWTの署名検証は通ったが、対応するユーザーが存在しない(403 user_not_provisioned、REST v1)/PermissionDenied(gRPC v2)」
// を返した場合に、backend 固有のエラー型に依存させず呼び出し元へ伝えるためのセンチネルエラー
//
// 【実機検証で発覚した不具合】
// admin画面でログイン中のユーザーを削除した場合、
// 削除後もbff側のRedisセッションは残ったままのため、frontendは「ログイン済み」のままタスク/ラベル画面へ遷移できてしまっていた
// 実際にTask/Label APIを呼んだ時点で backend がこのエラーを返すが、以前は ErrUpstreamUnauthorized として扱われず、
// 生のエラー文字列がそのままJSONでフロントに表示されてしまっていた
// (401であれば frontend の apiFetch が自動的に /login へリダイレクトする既存の仕組みに乗れたはずだった)
//
// リフレッシュを試みても対象ユーザー自体が存在しない以上意味が無いため、
// ErrUpstreamUnauthorized とは区別し、Refresher.Do でリフレッシュを試みず
// 即座にセッションを破棄する(ErrUpstreamUnauthorized と同じ最終的な401応答にする)
var ErrUserNotProvisioned = errors.New("upstream user not provisioned")

// TokenRefresher は Keycloak への refresh_token 交換だけを表す最小 interface
// *OIDCClient が実体だが、Refresher の単体テストでは実際の Keycloak なしに
// 「リフレッシュが成功/失敗したらどうなるか」
// を検証したいため抽象化する
type TokenRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (*TokenSet, error)
}

// Refresher はbackend呼び出し時のリアクティブなトークンリフレッシュを担う
// CONTRACT.md: 「backendが401を返した時点でBFFがrefresh_tokenで更新し、
// 元のリクエストを1回だけリトライする」を、REST(v1)/gRPC(v2)どちらの
// プロキシコードからも共通で使えるようにここへ集約する
type Refresher struct {
	store        *Store
	tokenRefresh TokenRefresher
}

func NewRefresher(store *Store, tokenRefresh TokenRefresher) *Refresher {
	return &Refresher{store: store, tokenRefresh: tokenRefresh}
}

// Do はsessionIDに紐づくaccess tokenを使ってfnを実行する
// fnがErrUpstreamUnauthorizedを返した場合のみ、分散ロックを取った上で
// refresh_tokenによる更新を試み、成功すれば新しいaccess tokenで1回だけ再実行する
// リフレッシュ自体が失敗した場合はセッションを削除し、呼び出し元へエラーを返す
// (ハンドラ側でこれを401としてクライアントに伝える)
func (r *Refresher) Do(ctx context.Context, sessionID string, fn func(accessToken string) error) error {
	sess, err := r.store.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	err = fn(sess.AccessToken)
	if err == nil {
		return nil
	}

	// ユーザー自体がbackend側に存在しない(admin画面での削除等)場合、
	// access tokenをリフレッシュしても対象ユーザーは戻ってこないため、
	// リフレッシュは試みずに即座にセッションを破棄する
	if errors.Is(err, ErrUserNotProvisioned) {
		_ = r.store.Delete(ctx, sessionID)
		return fmt.Errorf("ユーザーが存在しないためセッションを破棄しました: %w", err)
	}

	if !errors.Is(err, ErrUpstreamUnauthorized) {
		return err
	}

	// CONTRACT.mdセクション16.4: ローカル認証(HMAC/RSA)には refresh_token が存在しない(24時間の固定寿命で更新なし)
	// Keycloak 以外のセッションで401を受けた場合はリフレッシュを試みる意味が無いため、
	// 即座にセッションを破棄して呼び出し元(ハンドラ)へ401として返す(frontendは/loginへ再遷移する)
	if sess.AuthMode != "" && sess.AuthMode != AuthModeKeycloak {
		_ = r.store.Delete(ctx, sessionID)
		return fmt.Errorf("ローカル認証セッションのaccess tokenが失効したためセッションを破棄しました: %w", err)
	}

	// ここから先はaccess token期限切れの可能性があるため、同一セッションに対する
	// 同時多重リフレッシュを避けるロックの中で処理する
	var retryErr error
	lockErr := r.store.WithLock(ctx, sessionID, func(ctx context.Context) error {
		// ロック取得中に他のリクエストが既にリフレッシュ済みの可能性があるため、
		// 最新の状態を読み直してから「本当にまだ古いトークンのままか」を確認する
		latest, err := r.store.Get(ctx, sessionID)
		if err != nil {
			return err
		}
		if latest.AccessToken != sess.AccessToken {
			// 他のリクエストが既にリフレッシュ済み
			// 最新のaccess tokenで再試行する
			retryErr = fn(latest.AccessToken)
			return nil
		}

		newTokens, err := r.tokenRefresh.Refresh(ctx, latest.RefreshToken)
		if err != nil {
			// refresh_token自体が失効している
			// セッションを破棄しログイン画面へ戻ってもらうしかない
			_ = r.store.Delete(ctx, sessionID)
			retryErr = fmt.Errorf("リフレッシュに失敗したためセッションを破棄しました: %w", err)
			return nil
		}

		updated := *latest
		updated.AccessToken = newTokens.AccessToken
		updated.RefreshToken = newTokens.RefreshToken
		updated.IDToken = newTokens.IDToken
		updated.AccessTokenExp = newTokens.AccessTokenExp
		updated.RefreshTokenExp = newTokens.RefreshTokenExp
		if err := r.store.Update(ctx, sessionID, updated); err != nil {
			retryErr = fmt.Errorf("リフレッシュ後のセッション更新に失敗しました: %w", err)
			return nil
		}

		retryErr = fn(newTokens.AccessToken)
		return nil
	})
	if lockErr != nil {
		return fmt.Errorf("トークンリフレッシュのロック取得に失敗しました: %w", lockErr)
	}
	return retryErr
}
