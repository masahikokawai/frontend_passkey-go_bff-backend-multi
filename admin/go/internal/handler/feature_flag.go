// Package handler はFeature Flag管理画面のGinハンドラ群
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/service"
)

type FeatureFlagHandler struct {
	svc *service.FeatureFlagService
}

func NewFeatureFlagHandler(svc *service.FeatureFlagService) *FeatureFlagHandler {
	return &FeatureFlagHandler{svc: svc}
}

func parseUintParam(c *gin.Context, name string) (uint64, error) {
	return strconv.ParseUint(c.Param(name), 10, 64)
}

// Index は `GET /`
// フラグ一覧を表示する
func (h *FeatureFlagHandler) Index(c *gin.Context) {
	flags, err := h.svc.List(c.Request.Context())
	if err != nil {
		render(c, http.StatusInternalServerError, "pages/error", gin.H{"Error": err.Error()})
		return
	}
	render(c, http.StatusOK, "pages/index", gin.H{"Flags": flags})
}

// Edit は `GET /flags/:id/edit`
// 編集フォームを表示する
func (h *FeatureFlagHandler) Edit(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		render(c, http.StatusBadRequest, "pages/error", gin.H{"Error": "invalid id"})
		return
	}
	flag, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		render(c, http.StatusNotFound, "pages/error", gin.H{"Error": err.Error()})
		return
	}
	render(c, http.StatusOK, "pages/edit", gin.H{"Flag": flag})
}

// Update は `POST /flags/:id`
// 更新 + 監査ログ記録を行いフラグ一覧へ戻す
func (h *FeatureFlagHandler) Update(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		render(c, http.StatusBadRequest, "pages/error", gin.H{"Error": "invalid id"})
		return
	}

	// HTMLのcheckboxは未チェック時に値自体が送られてこないのがフォームの仕様
	// (Railsのcheck_boxがhidden fieldで"0"を仕込んで補うのと同じ問題)
	// ここではPostFormでの存在有無だけで判定するシンプルな実装にしている
	enabled := c.PostForm("enabled") == "on"
	defaultVariation := c.PostForm("default_variation")
	changedBy, _, _ := c.Request.BasicAuth()
	if changedBy == "" {
		changedBy = "unknown"
	}

	if err := h.svc.Update(c.Request.Context(), id, service.UpdateInput{
		Enabled:          enabled,
		DefaultVariation: defaultVariation,
		ChangedBy:        changedBy,
	}); err != nil {
		flag, _ := h.svc.Get(c.Request.Context(), id)
		render(c, http.StatusUnprocessableEntity, "pages/edit", gin.H{"Flag": flag, "Error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/")
}

// AuditLog は `GET /flags/:id/audit_log`
// 指定フラグの変更履歴一覧を表示する
func (h *FeatureFlagHandler) AuditLog(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		render(c, http.StatusBadRequest, "pages/error", gin.H{"Error": "invalid id"})
		return
	}
	flag, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		render(c, http.StatusNotFound, "pages/error", gin.H{"Error": err.Error()})
		return
	}
	logs, err := h.svc.AuditLogs(c.Request.Context(), id)
	if err != nil {
		render(c, http.StatusInternalServerError, "pages/error", gin.H{"Error": err.Error()})
		return
	}
	render(c, http.StatusOK, "pages/audit_log", gin.H{"Flag": flag, "Logs": logs})
}

func boolLabel(b *bool) string {
	if b == nil {
		return "-"
	}
	if *b {
		return "true"
	}
	return "false"
}
