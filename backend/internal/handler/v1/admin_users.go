package v1

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// RequireAdminInternalToken はadmin/go・admin/rails専用の軽量な認可ミドルウェア
//
// admin/go・admin/railsはユーザーのJWTを持たない(自分自身をBasic Authで守るだけの別アプリ)ため、通常のRequireAuthは使えない
// RequireFeatureFlagPollToken/RequireLocalAuthInternalTokenと同じ考え方の、また別の共有シークレットで認可する
// (CONTRACT.md セクション17.3)
func RequireAdminInternalToken(expectedToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader("X-Admin-Internal-Token")
		if got == "" || !secureTokenEqual(got, expectedToken) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

type createUserRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role" binding:"required"`
}

// CreateAdmin は POST /internal/v1/admin/users(CONTRACT.mdセクション17.2)
//
// admin画面から作成できるのはローカル認証ユーザーのみ
// Keycloak 経由のユーザーはJITプロビジョニング(セクション10)で初回ログイン時に自動作成されるため、ここでの事前作成は対象外
func (h *UserHandler) CreateAdmin(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 【実機検証で判明】以前はここに"message"キーが無く、admin/go・admin/railsの
		// 画面に空のエラーメッセージ("validation error: "のように文言が欠けた状態)が表示されていた
		// 他のエラー経路(renderServiceError)と同じく、必ず人間向けのメッセージを付与する
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_request", "message": "リクエストの形式が不正です"})
		return
	}
	role, err := parseRole(req.Role)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_role", "message": "roleは general または management を指定してください"})
		return
	}

	dto, err := h.users.Create(c.Request.Context(), req.Name, req.Email, req.Password, role)
	if err != nil {
		if errors.Is(err, service.ErrEmailTaken) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "email_taken", "message": "このメールアドレスは既に使われています"})
			return
		}
		renderServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"user_id": dto.ID,
		"name":    dto.Name,
		"email":   dto.Email,
		"role":    dto.Role,
	})
}

// DeleteAdmin は DELETE /internal/v1/admin/users/:id(CONTRACT.mdセクション17.3)
func (h *UserHandler) DeleteAdmin(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	if err := h.users.Delete(c.Request.Context(), id); err != nil {
		renderServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
