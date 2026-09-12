package authjwt

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequireExternalClientAuth はBFFを経由しない外部公開API(CONTRACT.mdセクション11)専用のミドルウェア
//
// JWKS検証そのものはRequireAuthと同じVerifierを再利用しつつ、
// Client Credentials Grantで発行されたトークンであることを追加で確認する
// (aud検証だけでは「BFFが転送してきたユーザーのトークン」と「外部クライアント自身の
// トークン」を区別できないため、azp(authorized party)クレームで発行先クライアントを見る)
func RequireExternalClientAuth(verifier TokenVerifier, expectedClientID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		claims, err := verifier.Verify(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			return
		}

		if claims.Azp != expectedClientID {
			// 既知の制約(CONTRACT.md参照): ここを通れば azp が一致する限り任意の user_id を指定してタスクを読める
			// エンドユーザー単位の認可は行わない、サーバー間の信頼関係を前提にした設計
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "client_not_allowed"})
			return
		}

		c.Set(claimsContextKey, claims)
		c.Next()
	}
}
