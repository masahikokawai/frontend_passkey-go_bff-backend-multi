package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// LabelHandler はLabel CRUD(v1 RESTのみ、gRPC化の対象外)
type LabelHandler struct {
	labels *service.LabelService
}

func NewLabelHandler(labels *service.LabelService) *LabelHandler {
	return &LabelHandler{labels: labels}
}

func (h *LabelHandler) List(c *gin.Context) {
	labels, err := h.labels.List(c.Request.Context())
	if err != nil {
		renderServiceError(c, err)
		return
	}
	out := make([]gin.H, 0, len(labels))
	for _, l := range labels {
		out = append(out, gin.H{"id": l.ID, "name": l.Name})
	}
	c.JSON(http.StatusOK, gin.H{"labels": out})
}

type labelRequestBody struct {
	Name string `json:"name" binding:"required"`
}

func (h *LabelHandler) Create(c *gin.Context) {
	var body labelRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	dto, err := h.labels.Create(c.Request.Context(), body.Name)
	if err != nil {
		renderServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": dto.ID, "name": dto.Name})
}

func (h *LabelHandler) Update(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	var body labelRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	dto, err := h.labels.Update(c.Request.Context(), id, body.Name)
	if err != nil {
		renderServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": dto.ID, "name": dto.Name})
}

func (h *LabelHandler) Delete(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	if err := h.labels.Delete(c.Request.Context(), id); err != nil {
		renderServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
