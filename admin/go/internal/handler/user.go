package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/client"
)

// userAPIClient はUserHandlerが必要とする最小のクライアント操作
// (テストではhttptestを使わず、fakeに差し替えられるようにする)
type userAPIClient interface {
	List(ctx context.Context) ([]client.User, error)
	Create(ctx context.Context, in client.CreateInput) (client.User, error)
	UpdateRole(ctx context.Context, id uint64, role string) error
	Delete(ctx context.Context, id uint64) error
}

// UserHandler はユーザー管理画面のGinハンドラ群(CONTRACT.mdセクション17.4)
type UserHandler struct {
	users userAPIClient
}

func NewUserHandler(users userAPIClient) *UserHandler {
	return &UserHandler{users: users}
}

// Index は `GET /users`
// ユーザー一覧 + 新規作成フォームを表示する
func (h *UserHandler) Index(c *gin.Context) {
	users, err := h.users.List(c.Request.Context())
	if err != nil {
		render(c, http.StatusInternalServerError, "pages/error", gin.H{"Error": err.Error()})
		return
	}
	render(c, http.StatusOK, "pages/users_index", gin.H{"Users": users})
}

// Create は `POST /users`
// ローカル認証ユーザーを新規作成する
// (CONTRACT.mdセクション17.2: 作成できるのはローカル認証ユーザーのみ)
func (h *UserHandler) Create(c *gin.Context) {
	_, err := h.users.Create(c.Request.Context(), client.CreateInput{
		Name:     c.PostForm("name"),
		Email:    c.PostForm("email"),
		Password: c.PostForm("password"),
		Role:     c.PostForm("role"),
	})
	if err != nil {
		h.renderIndexWithError(c, http.StatusUnprocessableEntity, err)
		return
	}
	c.Redirect(http.StatusFound, "/users")
}

// UpdateRole は `POST /users/:id/role`
// roleを変更する
// (HTMLフォームからのPOSTのため、backend APIはPATCHだがこちらはPOSTで受ける)
func (h *UserHandler) UpdateRole(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		render(c, http.StatusBadRequest, "pages/error", gin.H{"Error": "invalid id"})
		return
	}
	if err := h.users.UpdateRole(c.Request.Context(), id, c.PostForm("role")); err != nil {
		h.renderIndexWithError(c, statusForClientError(err), err)
		return
	}
	c.Redirect(http.StatusFound, "/users")
}

// Delete は `POST /users/:id/delete`
// ユーザーを削除する
func (h *UserHandler) Delete(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		render(c, http.StatusBadRequest, "pages/error", gin.H{"Error": "invalid id"})
		return
	}
	if err := h.users.Delete(c.Request.Context(), id); err != nil {
		h.renderIndexWithError(c, statusForClientError(err), err)
		return
	}
	c.Redirect(http.StatusFound, "/users")
}

// renderIndexWithError は一覧を再取得したうえでエラーメッセージ付きで表示する
// (作成フォーム・role変更・削除いずれの失敗でも、ユーザーは一覧画面に留まる)
func (h *UserHandler) renderIndexWithError(c *gin.Context, status int, err error) {
	users, listErr := h.users.List(c.Request.Context())
	if listErr != nil {
		render(c, http.StatusInternalServerError, "pages/error", gin.H{"Error": listErr.Error()})
		return
	}
	render(c, status, "pages/users_index", gin.H{"Users": users, "Error": err.Error()})
}

func statusForClientError(err error) int {
	switch {
	case errors.Is(err, client.ErrLastManager):
		return http.StatusConflict
	case errors.Is(err, client.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, client.ErrValidation):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
