package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CSRFMiddleware はDouble Submit Cookie方式のCSRF対策
// GET/HEAD/OPTIONSは対象外(副作用がない前提のRESTの規約に従う)
// それ以外のメソッドでは、csrf_token Cookie の値と `X-CSRF-Token` ヘッダの値が一致することを要求する
// 値そのものは秘匿情報ではない(むしろJSから読める必要がある)ため、一致検証のみを行う
//
// Cookie自体の発行(初回値のセット)はログイン成功時・/api/me応答時に行う
// (handler.goのIssueCSRFCookie参照)
func CSRFMiddleware(cfg CookieConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

		// ローカルログイン(POST /api/auth/login, /api/auth/login/rsa)はまだセッションが
		// 存在せず、csrf_token Cookieも発行されていない初回アクセス時に叩かれるため
		// 対象外にする(CONTRACT.mdセクション16.6)
		// ログイン成功後に発行される
		// セッションCookie自体を偽装することはできないため、CSRFの主目的である
		// 「既存セッションを使った意図しない操作の防止」には影響しない
		//
		// パスキーのlogin/begin・login/finish(CONTRACT.mdセクション22.5)も同じ理由で対象外にする
		// (register/begin・register/finishは要ログインのためCSRF対象のまま)
		switch c.Request.URL.Path {
		case "/api/auth/login", "/api/auth/login/rsa",
			"/api/auth/passkey/login/begin", "/api/auth/passkey/login/finish":
			c.Next()
			return
		}

		cookieValue, err := c.Cookie(cfg.CSRFCookieName)
		if err != nil || cookieValue == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "csrf_token cookie is missing"})
			return
		}
		headerValue := c.GetHeader("X-CSRF-Token")
		if headerValue == "" || headerValue != cookieValue {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "csrf token mismatch"})
			return
		}
		c.Next()
	}
}

// IssueCSRFCookie はcsrf_tokenをブラウザに発行する
// 既にCookieが存在する場合はその値を使い回す(タブを複数開いている場合に、一方の/api/me呼び出しでもう一方の
// 進行中リクエストのトークンを無効化してしまわないようにするため)
// HttpOnly=false: ReactがJSから読み取ってX-CSRF-Tokenヘッダへ転記する必要があるため
func IssueCSRFCookie(c *gin.Context, cfg CookieConfig) (string, error) {
	if existing, err := c.Cookie(cfg.CSRFCookieName); err == nil && existing != "" {
		return existing, nil
	}
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cfg.CSRFCookieName, token, 0, "/", "", cfg.Secure, false)
	return token, nil
}
