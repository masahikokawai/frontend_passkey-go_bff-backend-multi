package external

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
)

// Handlers は外部公開APIのハンドラ一式
type Handlers struct {
	Task *TaskHandler
}

// requestLogger は全リクエストをmethod/path/status/durationでログする
// internal/handler/v1/router.goのrequestLoggerと全く同じロジック(パッケージ間で
// 共有していないのは既存コードの都合であり、意図的な重複ではない)
//
// 【実機検証で判明・追記】以前はREST v1にしかこのミドルウェアが無く、外部公開APIは
// 起動時のログしか出ないため、実際にどのリクエストを処理したかログから確認できなかった
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	}
}

// NewRouter は外部公開API専用のGinルーターを組み立てる
// REST v1(internal/handler/v1)とは別ポート・別ミドルウェア(RequireExternalClientAuth)
func NewRouter(h Handlers, verifier authjwt.TokenVerifier, expectedClientID string, logger *slog.Logger) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger(logger))

	ext := router.Group("/external/v1")
	ext.Use(authjwt.RequireExternalClientAuth(verifier, expectedClientID))
	ext.Use(clientIDToContext())
	{
		ext.GET("/tasks", h.Task.List)
	}

	return router
}

// clientIDToContext はRequireExternalClientAuthが検証済みのclaimsから
// azp(client_id)を取り出し、TaskHandlerがFeature Flagのtargeting keyとして
// 使えるようgin.Contextに積む
func clientIDToContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := authjwt.ClaimsFromContext(c)
		if ok {
			c.Set("external_client_id", claims.Azp)
		}
		c.Next()
	}
}
