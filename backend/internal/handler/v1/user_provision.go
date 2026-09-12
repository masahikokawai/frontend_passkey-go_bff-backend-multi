package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// UserHandler はJITプロビジョニング・Admin::Users相当のエンドポイント
type UserHandler struct {
	users *service.UserService
}

func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

// Provision は POST /internal/v1/users/provision
//
// リクエストボディは受け取らない
//
// keycloak_sub/name/email/roles は、このハンドラの手前で通っているRequireAuthミドルウェアが検証済みのJWTクレームから取り出す
// (BFFが送ってきた値を信頼せず、署名検証済みのトークンだけを信頼する設計CONTRACT.md参照)
// BFFの `/api/auth/callback` から、認可コード交換直後に呼ばれる想定
func (h *UserHandler) Provision(c *gin.Context) {
	claims, ok := authjwt.ClaimsFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	dto, err := h.users.Provision(c.Request.Context(), claims.Subject, claims.Name, claims.Email, claims.RealmAccess.Roles)
	if err != nil {
		renderServiceError(c, err)
		return
	}
	// キー名はCONTRACT.mdセクション5.1・bffのprovisionResponseBodyに合わせて user_id とする
	c.JSON(http.StatusOK, gin.H{
		"user_id": dto.ID,
		"name":    dto.Name,
		"email":   dto.Email,
		"role":    dto.Role,
	})
}

// List は GET /internal/v1/users(Admin::Users#index相当)
func (h *UserHandler) List(c *gin.Context) {
	users, err := h.users.List(c.Request.Context())
	if err != nil {
		renderServiceError(c, err)
		return
	}
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		out = append(out, gin.H{"id": u.ID, "name": u.Name, "email": u.Email, "role": u.Role, "has_passkey": u.HasPasskey})
	}
	c.JSON(http.StatusOK, gin.H{"users": out})
}

type updateRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

// UpdateRole は PATCH /internal/v1/users/:id/role(Admin::Users#update相当)
func (h *UserHandler) UpdateRole(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	role, err := parseRole(req.Role)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_role"})
		return
	}
	if err := h.users.UpdateRole(c.Request.Context(), id, role); err != nil {
		renderServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
