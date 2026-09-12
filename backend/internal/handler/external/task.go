// Package external はCONTRACT.mdセクション11のBFF非経由・外部公開API
// REST v1(internal/handler/v1)とは別ミドルウェア(RequireExternalClientAuth)・
// 別ポートで待ち受けるため、意図的にパッケージを分けている
package external

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// FlagEvaluator は backend.external-tasks-pagination-v2 / backend.external-tasks-orm を
// 評価するための最小interface
// 実体は internal/featureflag.Evaluator だが、handler層がfeatureflagパッケージへ
// 直接依存しないよう構造的部分型として宣言するだけに留める
//
// 【CONTRACT.mdセクション24で追加】StringValueは backend.external-tasks-orm(gorm/bob、
// 多値フラグ)の評価に使う。既存のbackend.external-tasks-pagination-v2(bool)とは
// 直交する別軸なので、掛け合わせで4通りの組み合わせを解決する(下のList参照)
type FlagEvaluator interface {
	BoolValue(ctx context.Context, flagKey string, defaultValue bool, targetingKey string) bool
	StringValue(ctx context.Context, flagKey string, defaultValue string, targetingKey string) string
}

// TaskHandler は /external/v1/tasks を提供する
type TaskHandler struct {
	tasks  *service.TaskService
	flags  FlagEvaluator
	logger *slog.Logger
}

func NewTaskHandler(tasks *service.TaskService, flags FlagEvaluator, logger *slog.Logger) *TaskHandler {
	return &TaskHandler{tasks: tasks, flags: flags, logger: logger}
}

const (
	flagKeyExternalPaginationV2 = "backend.external-tasks-pagination-v2"
	// 【CONTRACT.mdセクション24で追加】backend.external-tasks-pagination-v2(offset/cursor)とは
	// 独立した2軸目のFeature Flag。GORM実装/bob実装のどちらでTask一覧を取得するかを切り替える
	// (migrations/000014_add_external_tasks_orm_flag)
	flagKeyExternalOrm  = "backend.external-tasks-orm"
	ormVariationGorm    = "gorm"
	ormVariationBob     = "bob"
	defaultOrmVariation = ormVariationGorm
)

// List は GET /external/v1/tasks
// 直交する2つのFeature Flagを評価し、4通りの組み合わせ(offset×gorm/offset×bob/
// cursor×gorm/cursor×bob)を解決する(CONTRACT.mdセクション11・24)
//   - backend.external-tasks-pagination-v2(bool): offsetページング(v1)/keyset・cursorページング(v2)
//   - backend.external-tasks-orm(gorm/bob): Task一覧取得をGORM/bobのどちらで行うか
//
// 4パターンともレスポンスのJSON形状は完全に同一(内部実装の入れ替えのみ)
func (h *TaskHandler) List(c *gin.Context) {
	userIDStr := c.Query("user_id")
	if userIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	// targeting keyはAPIクライアントのclient_id(azpクレーム、RequireExternalClientAuthが
	// 検証済みのものをそのまま使う)。2つのフラグとも同じtargeting keyで評価する
	clientID, _ := c.Get("external_client_id")
	clientIDStr, _ := clientID.(string)

	useV2 := h.flags.BoolValue(c.Request.Context(), flagKeyExternalPaginationV2, false, clientIDStr)
	orm := h.flags.StringValue(c.Request.Context(), flagKeyExternalOrm, defaultOrmVariation, clientIDStr)
	useBob := orm == ormVariationBob

	if h.logger != nil {
		impl := "v1(offsetページング)"
		if useV2 {
			impl = "v2(keysetページング)"
		}
		h.logger.Info("backend.external-tasks-pagination-v2/backend.external-tasks-ormの評価結果により実装を振り分け",
			slog.String("client_id", clientIDStr), slog.Bool("use_v2", useV2), slog.String("implementation", impl),
			slog.String("orm", orm))
	}

	switch {
	case useV2 && useBob:
		h.listV2Bob(c, userID)
	case useV2:
		h.listV2(c, userID)
	case useBob:
		h.listV1Bob(c, userID)
	default:
		h.listV1(c, userID)
	}
}

// listV1 はoffsetページング(旧実装)・GORM実装
func (h *TaskHandler) listV1(c *gin.Context, userID uint64) {
	page := queryIntDefault(c, "page", 1)
	pageSize := queryIntDefault(c, "page_size", 10)

	dtos, total, err := h.tasks.ListExternalV1(c.Request.Context(), service.ExternalListFilterV1{
		UserID:   userID,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	respondV1(c, dtos, page, pageSize, total)
}

// listV1Bob はlistV1のbob実装版(CONTRACT.mdセクション24)
// listV1と処理は同じで、呼び出すservice関数(ListExternalV1Bob)だけが違う
// (JSON組み立てはrespondV1に共通化しているため、挙動・レスポンス形状はlistV1と完全に一致する)
func (h *TaskHandler) listV1Bob(c *gin.Context, userID uint64) {
	page := queryIntDefault(c, "page", 1)
	pageSize := queryIntDefault(c, "page_size", 10)

	dtos, total, err := h.tasks.ListExternalV1Bob(c.Request.Context(), service.ExternalListFilterV1{
		UserID:   userID,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	respondV1(c, dtos, page, pageSize, total)
}

// listV2 はkeyset(cursor)ページング(新実装)・GORM実装
func (h *TaskHandler) listV2(c *gin.Context, userID uint64) {
	cursor := c.Query("cursor")
	limit := queryIntDefault(c, "limit", 10)

	dtos, nextCursor, err := h.tasks.ListExternalV2(c.Request.Context(), service.ExternalListFilterV2{
		UserID: userID,
		Cursor: cursor,
		Limit:  limit,
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	respondV2(c, dtos, nextCursor, limit)
}

// listV2Bob はlistV2のbob実装版(CONTRACT.mdセクション24)
// listV2と処理は同じで、呼び出すservice関数(ListExternalV2Bob)だけが違う
// (JSON組み立てはrespondV2に共通化しているため、挙動・レスポンス形状はlistV2と完全に一致する)
func (h *TaskHandler) listV2Bob(c *gin.Context, userID uint64) {
	cursor := c.Query("cursor")
	limit := queryIntDefault(c, "limit", 10)

	dtos, nextCursor, err := h.tasks.ListExternalV2Bob(c.Request.Context(), service.ExternalListFilterV2{
		UserID: userID,
		Cursor: cursor,
		Limit:  limit,
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	respondV2(c, dtos, nextCursor, limit)
}

// respondV1/respondV2 はGORM実装・bob実装のどちらが取得したTaskDTOでも同じJSON形状を
// 組み立てる共通処理(CONTRACT.mdセクション24: 「レスポンスのJSON形状は4パターンとも完全に
// 同一」という要件をコードの構造としても保証するため、JSON組み立てを1箇所にまとめている)
func respondV1(c *gin.Context, dtos []service.TaskDTO, page, pageSize int, total int64) {
	tasksJSON := make([]gin.H, 0, len(dtos))
	for _, dto := range dtos {
		tasksJSON = append(tasksJSON, taskDTOToJSON(dto))
	}
	c.JSON(http.StatusOK, gin.H{
		"tasks":     tasksJSON,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	})
}

func respondV2(c *gin.Context, dtos []service.TaskDTO, nextCursor string, limit int) {
	tasksJSON := make([]gin.H, 0, len(dtos))
	for _, dto := range dtos {
		tasksJSON = append(tasksJSON, taskDTOToJSON(dto))
	}
	var nextCursorJSON any
	if nextCursor != "" {
		nextCursorJSON = nextCursor
	}
	c.JSON(http.StatusOK, gin.H{
		"tasks":       tasksJSON,
		"next_cursor": nextCursorJSON,
		"limit":       limit,
	})
}

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
	v := c.Query(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}
