package v1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// userLookup はTaskHandlerがJWTのclaimsからアプリ内部のユーザーを引くために必要な最小のインターフェース
// *repository.Userがこれらのメソッドを実装しているため
// 本番の配線(cmd/server/main.go)は変更不要だが、テストではfakeに差し替えられる
//
// 【セクション16.5で追加】Getはローカル(HMAC/RSA)発行のJWT用
// GetByKeycloakSub は Keycloak 発行の JWT用(resolveUserID参照)
type userLookup interface {
	GetByKeycloakSub(ctx context.Context, keycloakSub string) (*model.User, error)
	Get(ctx context.Context, id uint64) (*model.User, error)
}

// TaskHandler はREST v1(旧実装)のTaskエンドポイント
// ListだけがN+1を再現するservice.ListLegacyを呼ぶ(CONTRACT.md参照)
type TaskHandler struct {
	tasks *service.TaskService
	users userLookup
}

func NewTaskHandler(tasks *service.TaskService, users userLookup) *TaskHandler {
	return &TaskHandler{tasks: tasks, users: users}
}

// resolveUserID はJWT検証済みclaimsのsubから内部ユーザーIDを引く(gRPC v2と同じ考え方)
//
// 【セクション16.5で変更】claims.Issuerで発行元を判定し、ローカル(HMAC/RSA)発行の
// JWTはsubに内部user_idそのものが入っている(bffが発行時にそう設定する
// CONTRACT.mdセクション16.4)ためGetで直接引き、Keycloak発行のJWTは従来通り
// subがkeycloak_subなのでGetByKeycloakSubで引く
func (h *TaskHandler) resolveUserID(c *gin.Context) (uint64, bool) {
	claims, ok := authjwt.ClaimsFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return 0, false
	}

	if authjwt.IsLocalIssuer(claims.Issuer) {
		id, err := strconv.ParseUint(claims.Subject, 10, 64)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "user_not_provisioned"})
			return 0, false
		}
		user, err := h.users.Get(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "user_not_provisioned"})
			return 0, false
		}
		return user.ID, true
	}

	user, err := h.users.GetByKeycloakSub(c.Request.Context(), claims.Subject)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "user_not_provisioned"})
		return 0, false
	}
	return user.ID, true
}

// List は GET /internal/v1/tasks
//
// 【N+1が発生する箇所】ここが呼ぶ service.ListLegacy は、一覧取得後にタスクごとに
// ラベルを個別取得する旧実装であり、意図的にN+1を再現している(CONTRACT.md参照)
func (h *TaskHandler) List(c *gin.Context) {
	userID, ok := h.resolveUserID(c)
	if !ok {
		return
	}

	filter := service.TaskListFilterV1{
		Name:           c.Query("name"),
		SortFinishedOn: c.Query("sort"),
		Limit:          queryIntDefault(c, "limit", 20),
		Offset:         queryIntDefault(c, "offset", 0),
		LabelIDs:       parseUintListQuery(c, "label_ids"),
	}
	if s := c.Query("status"); s != "" {
		st, err := model.TaskStatusFromString(s)
		if err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_status"})
			return
		}
		filter.Status = &st
	}

	dtos, total, err := h.tasks.ListLegacy(c.Request.Context(), userID, filter)
	if err != nil {
		renderServiceError(c, err)
		return
	}

	tasksJSON := make([]gin.H, 0, len(dtos))
	for _, dto := range dtos {
		tasksJSON = append(tasksJSON, taskDTOToJSON(dto))
	}
	c.JSON(http.StatusOK, gin.H{
		"tasks":  tasksJSON,
		"total":  total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}

func (h *TaskHandler) Get(c *gin.Context) {
	userID, ok := h.resolveUserID(c)
	if !ok {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	dto, err := h.tasks.Get(c.Request.Context(), id, userID)
	if err != nil {
		renderServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, taskDTOToJSON(dto))
}

// json タグはCONTRACT.md セクション5.1(bffのtaskV1RequestBodyと合わせたスネークケース)に従う
// bffのresty呼び出しはこのケースで送信するため、ここをcamelCaseにするとbinding:"required"が
// 常に失敗し、Create/Updateがすべて400になる(実際に統合時に発覚した不整合)
type taskRequestBody struct {
	Name        string   `json:"name" binding:"required"`
	Description *string  `json:"description"`
	Status      string   `json:"status" binding:"required"`
	FinishedOn  string   `json:"finished_on" binding:"required"`
	LabelIDs    []uint64 `json:"label_ids"`
}

func (h *TaskHandler) Create(c *gin.Context) {
	userID, ok := h.resolveUserID(c)
	if !ok {
		return
	}
	var body taskRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	finishedOn, err := time.Parse("2006-01-02", body.FinishedOn)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_finished_on"})
		return
	}
	dto, err := h.tasks.Create(c.Request.Context(), userID, service.TaskInput{
		Name:        body.Name,
		Description: body.Description,
		Status:      body.Status,
		FinishedOn:  finishedOn,
		LabelIDs:    body.LabelIDs,
	})
	if err != nil {
		renderServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, taskDTOToJSON(dto))
}

func (h *TaskHandler) Update(c *gin.Context) {
	userID, ok := h.resolveUserID(c)
	if !ok {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	var body taskRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	finishedOn, err := time.Parse("2006-01-02", body.FinishedOn)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_finished_on"})
		return
	}
	dto, err := h.tasks.Update(c.Request.Context(), id, userID, service.TaskInput{
		Name:        body.Name,
		Description: body.Description,
		Status:      body.Status,
		FinishedOn:  finishedOn,
		LabelIDs:    body.LabelIDs,
	})
	if err != nil {
		renderServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, taskDTOToJSON(dto))
}

func (h *TaskHandler) Delete(c *gin.Context) {
	userID, ok := h.resolveUserID(c)
	if !ok {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	if err := h.tasks.Delete(c.Request.Context(), id, userID); err != nil {
		renderServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// CONTRACT.md セクション5.1のJSON形状(スネークケース)に合わせる
// bffのtaskV1DTOはこのケースでアンマーシャルするため、camelCaseで返すと
// finished_on/created_at/updated_atがすべて空文字になる(統合時に発覚した不整合)
func taskDTOToJSON(dto service.TaskDTO) gin.H {
	labels := make([]gin.H, 0, len(dto.Labels))
	for _, l := range dto.Labels {
		labels = append(labels, gin.H{"id": l.ID, "name": l.Name})
	}
	return gin.H{
		"id":          dto.ID,
		"name":        dto.Name,
		"description": dto.Description,
		"status":      dto.Status,
		"finished_on": dto.FinishedOn.Format("2006-01-02"),
		"labels":      labels,
		"created_at":  dto.CreatedAt.Format(time.RFC3339),
		"updated_at":  dto.UpdatedAt.Format(time.RFC3339),
	}
}

func queryIntDefault(c *gin.Context, name string, def int) int {
	v, ok := parseUintQuery(c, name)
	if !ok {
		return def
	}
	return int(v)
}
