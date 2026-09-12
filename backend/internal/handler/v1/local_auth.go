package v1

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// localAuthService はLocalAuthHandlerが必要とする最小のservice操作
type localAuthService interface {
	VerifyLocalPassword(ctx context.Context, email, password string) (service.LocalAuthResult, error)
}

// LocalAuthHandler は POST /internal/v1/auth/verify-local-password を提供する
// CONTRACT.md セクション16.3: bffはDBを直接見ないため、ローカル(非Keycloak)認証の
// パスワード照合はこのエンドポイント経由で行う
type LocalAuthHandler struct {
	auth localAuthService
}

func NewLocalAuthHandler(auth localAuthService) *LocalAuthHandler {
	return &LocalAuthHandler{auth: auth}
}

type verifyLocalPasswordRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// VerifyPassword はemail/passwordを検証し、成功時のみ内部user_id/name/email/rolesを返す
// bffはこのレスポンスを元に自分でJWT(sub=user_id)を発行する(CONTRACT.mdセクション16.4)
func (h *LocalAuthHandler) VerifyPassword(c *gin.Context) {
	var body verifyLocalPasswordRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	result, err := h.auth.VerifyLocalPassword(c.Request.Context(), body.Email, body.Password)
	if err != nil {
		if errors.Is(err, service.ErrPasswordExpired) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "password_expired"})
			return
		}
		// email不存在・bcrypt不一致はいずれも invalid_credentials に丸める
		// (存在有無を返さない。service.LocalAuthService参照)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id": result.UserID,
		"name":    result.Name,
		"email":   result.Email,
		"roles":   result.Roles,
	})
}

// RequireLocalAuthInternalToken はverify-local-password専用の軽量な認可ミドルウェア
// このエンドポイントはログイン処理そのもの(まだJWTが存在しない)ため、通常のRequireAuthは使えない
// RequireFeatureFlagPollTokenと同じ考え方の別の共有シークレットで認可する
// (CONTRACT.md セクション16.3)
func RequireLocalAuthInternalToken(expectedToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader("X-Local-Auth-Internal-Token")
		if got == "" || !secureTokenEqual(got, expectedToken) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
