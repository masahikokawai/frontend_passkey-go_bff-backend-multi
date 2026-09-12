package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/bff-gin/bff/internal/auth"
)

// FlagEvaluator は backend.task-language / backend.task-protocol を評価するための最小interface
type FlagEvaluator interface {
	StringValue(ctx context.Context, flagKey string, defaultValue string, userID string) string
}

const (
	defaultTaskLanguage = "go"
	defaultTaskProtocol = "rest"
)

// TaskRoutes は /api/tasks* のハンドラをまとめたもの
// CONTRACT.mdセクション20: 言語(`backend.task-language`: go/rust/scala-http4s/scala-pekko/rails)と
// プロトコル(`backend.task-protocol`: rest/grpc)は直交する2軸のため、それぞれ別のFeature Flagで
// 評価し、組み合わせキー("go:rest"等)でClientsから実装を選ぶ
// どちらのフラグもBFF内部でのみ評価し、Reactには公開しない
type TaskRoutes struct {
	// Clients はキー"{backend.task-languageの値}:{backend.task-protocolの値}"で
	// TaskBackendClientを引くマップ(例: "go:rest", "go:grpc")
	// backend.task-language は段階的に実装を追加していく前提のため、未実装の言語が
	// 指定された場合はpickClientがgoへフォールバックする
	Clients   map[string]TaskBackendClient
	Flags     FlagEvaluator
	Refresher *auth.Refresher
	// Logger はnilでも動作する
	// 未設定時はログ出力しない
	// 既存テストが Logger を渡さずに構築しているケースへの後方互換のため
	Logger *slog.Logger
}

const defaultLimit = 20

// pickClient は backend.task-language / backend.task-protocol の評価とClients振り分けを兼ねる箇所
// admin/go・admin/railsでDBの値を変えたときに実際にどの実装が使われたか
// ログから追えるよう、振り分け結果を必ず記録する
func (t *TaskRoutes) pickClient(ctx context.Context, userID uint64) TaskBackendClient {
	uid := strconv.FormatUint(userID, 10)
	language := t.Flags.StringValue(ctx, "backend.task-language", defaultTaskLanguage, uid)
	protocol := t.Flags.StringValue(ctx, "backend.task-protocol", defaultTaskProtocol, uid)
	key := language + ":" + protocol

	client, ok := t.Clients[key]
	if !ok {
		fallbackKey := defaultTaskLanguage + ":" + protocol
		fallbackClient, fbOK := t.Clients[fallbackKey]
		if !fbOK {
			fallbackKey = defaultTaskLanguage + ":" + defaultTaskProtocol
			fallbackClient = t.Clients[fallbackKey]
		}
		if t.Logger != nil {
			t.Logger.Warn("backend.task-languageが未実装の組み合わせを指しているためgoへフォールバック",
				slog.Uint64("user_id", userID), slog.String("requested_key", key), slog.String("fallback_key", fallbackKey))
		}
		key, client = fallbackKey, fallbackClient
	}
	if t.Logger != nil {
		t.Logger.Info("backend.task-language/backend.task-protocolの評価結果によりTaskの実装を振り分け",
			slog.Uint64("user_id", userID), slog.String("language", language), slog.String("protocol", protocol), slog.String("implementation", key))
	}
	return client
}

// do はauth.Refresherを介してbackend呼び出しを実行し、401ならリフレッシュ後に1回だけ再試行する
// backend呼び出し自体のエラーはHTTPレスポンスへ変換する
func (t *TaskRoutes) do(c *gin.Context, sessionID string, fn func(accessToken string) error) bool {
	err := t.Refresher.Do(c.Request.Context(), sessionID, fn)
	if err == nil {
		return true
	}
	if errors.Is(err, auth.ErrUpstreamUnauthorized) || errors.Is(err, auth.ErrSessionNotFound) || errors.Is(err, auth.ErrUserNotProvisioned) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return false
	}
	c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
	return false
}

func (t *TaskRoutes) List(c *gin.Context) {
	sess, sessionID, ok := auth.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	limit := parseIntDefault(c.Query("limit"), defaultLimit)
	offset := parseIntDefault(c.Query("offset"), 0)
	filter := TaskFilter{
		Name:           c.Query("name"),
		Status:         c.Query("status"),
		SortFinishedOn: c.Query("sort"),
		Limit:          limit,
		Offset:         offset,
	}

	client := t.pickClient(c.Request.Context(), sess.UserID)
	var result TaskListResult
	ok = t.do(c, sessionID, func(accessToken string) error {
		var err error
		result, err = client.List(c.Request.Context(), accessToken, sess.UserID, filter)
		return err
	})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, result)
}

func (t *TaskRoutes) Create(c *gin.Context) {
	sess, sessionID, ok := auth.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var input TaskInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client := t.pickClient(c.Request.Context(), sess.UserID)
	var task Task
	ok = t.do(c, sessionID, func(accessToken string) error {
		var err error
		task, err = client.Create(c.Request.Context(), accessToken, sess.UserID, input)
		return err
	})
	if !ok {
		return
	}
	c.JSON(http.StatusCreated, task)
}

func (t *TaskRoutes) Get(c *gin.Context) {
	sess, sessionID, ok := auth.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	client := t.pickClient(c.Request.Context(), sess.UserID)
	var task Task
	ok = t.do(c, sessionID, func(accessToken string) error {
		var err error
		task, err = client.Get(c.Request.Context(), accessToken, sess.UserID, id)
		return err
	})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, task)
}

func (t *TaskRoutes) Update(c *gin.Context) {
	sess, sessionID, ok := auth.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var input TaskInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client := t.pickClient(c.Request.Context(), sess.UserID)
	var task Task
	ok = t.do(c, sessionID, func(accessToken string) error {
		var err error
		task, err = client.Update(c.Request.Context(), accessToken, sess.UserID, id, input)
		return err
	})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, task)
}

func (t *TaskRoutes) Delete(c *gin.Context) {
	sess, sessionID, ok := auth.CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	client := t.pickClient(c.Request.Context(), sess.UserID)
	ok = t.do(c, sessionID, func(accessToken string) error {
		return client.Delete(c.Request.Context(), accessToken, sess.UserID, id)
	})
	if !ok {
		return
	}
	c.Status(http.StatusNoContent)
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
