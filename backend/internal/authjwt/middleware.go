package authjwt

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const claimsContextKey = "authjwt_claims"

// RequireAuth はREST v1(Gin)向けのミドルウェア
// BFFがAuthorizationヘッダで転送してきたAccess Tokenを検証する
// 失敗時は必ずHTTP 401を返す(BFFはこの401を見てリアクティブにリフレッシュを試みる
// CONTRACT.mdセクション2)
//
// クライアントへは詳細を明かさず一律 "invalid_token" を返すが、原因調査ができるよう
// 実際の検証エラー(iss/aud不一致、署名エラー等)はサーバー側ログにだけ残す
// (以前はここを握りつぶしており、401の原因がbackend側のログから一切わからなかった)
func RequireAuth(verifier TokenVerifier, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" {
			logger.Warn("Authorizationヘッダが無い、またはBearer形式ではない",
				slog.String("path", c.Request.URL.Path))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		claims, err := verifier.Verify(token)
		if err != nil {
			logger.Warn("JWT検証に失敗した",
				slog.String("path", c.Request.URL.Path),
				slog.Any("error", err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			return
		}

		c.Set(claimsContextKey, claims)
		c.Next()
	}
}

// ClaimsFromContext はhandler層でログイン中ユーザーの情報を取り出すためのヘルパー
func ClaimsFromContext(c *gin.Context) (*Claims, bool) {
	v, ok := c.Get(claimsContextKey)
	if !ok {
		return nil, false
	}
	claims, ok := v.(*Claims)
	return claims, ok
}

// SetClaimsForTesting はテスト専用のヘルパー
//
// 実際の RequireAuth ミドルウェア
// (本物のJWT検証)を経由せずに、handler 層の単体テストで gin.Context へ Claims を直接注入するために使う
// 本番コードから呼び出すことは無い
func SetClaimsForTesting(c *gin.Context, claims *Claims) {
	c.Set(claimsContextKey, claims)
}
