package v1

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
)

// Handlers はREST v1のルーティングに必要な全ハンドラをまとめたもの
type Handlers struct {
	Task        *TaskHandler
	Label       *LabelHandler
	User        *UserHandler
	FeatureFlag *FeatureFlagExportHandler
	LocalAuth   *LocalAuthHandler
	Webauthn    *WebauthnHandler
}

// requestLogger は全リクエストをmethod/path/status/durationでログする
// 以前はこれが無く、backendへのリクエストが届いているかどうかすら
// ログから確認できなかった(bffのrequestLoggerと同じ考え方)
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

// NewRouter はGinのルーターを組み立てる
// 全ルートがRequireAuth配下にある
// (backendはprivate network限定でBFF経由のみ公開されるため、BFFが転送してきた
// Access Tokenの検証を全エンドポイントで必須にする。CONTRACT.mdセクション5)
func NewRouter(h Handlers, verifier authjwt.TokenVerifier, logger *slog.Logger, featureFlagPollToken, localAuthInternalToken, adminInternalToken, webauthnInternalToken string) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger(logger))

	// feature-flags/exportは通常のRequireAuth(v1グループ)には入れない
	// ユーザーセッションに紐づかない定期ポーリング(bffのGO Feature Flag HTTP retriever)
	// のためのエンドポイントで、共有シークレットヘッダで認可する専用方式のため
	// (CONTRACT.mdセクション13、RequireFeatureFlagPollToken参照)
	router.GET("/internal/v1/feature-flags/export",
		RequireFeatureFlagPollToken(featureFlagPollToken),
		h.FeatureFlag.Export,
	)

	// verify-local-passwordも同じ理由(まだセッション/JWTが無い呼び出し)で
	// RequireAuthではなく別の共有シークレットヘッダで認可する
	// (CONTRACT.md セクション16.3、RequireLocalAuthInternalToken参照)
	router.POST("/internal/v1/auth/verify-local-password",
		RequireLocalAuthInternalToken(localAuthInternalToken),
		h.LocalAuth.VerifyPassword,
	)

	// admin/go・admin/rails専用のユーザー管理ルート(CONTRACT.mdセクション17.3)
	// 通常のRequireAuth(v1グループ)には入れない(admin側にユーザーのJWTという
	// 概念が存在しないため、上記2つと同じ共有シークレットヘッダ方式で認可する)
	adminUsers := router.Group("/internal/v1/admin/users")
	adminUsers.Use(RequireAdminInternalToken(adminInternalToken))
	{
		adminUsers.GET("", h.User.List)
		adminUsers.POST("", h.User.CreateAdmin)
		adminUsers.PATCH("/:id/role", h.User.UpdateRole)
		adminUsers.DELETE("/:id", h.User.DeleteAdmin)
	}

	// パスキーでのログイン試行中(まだJWTが存在しない)にbffが呼ぶ2ルートも、
	// 上記2つと同じ共有シークレットヘッダ方式で認可する(CONTRACT.mdセクション22.4)
	webauthnInternal := router.Group("/internal/v1/auth/webauthn/credentials")
	webauthnInternal.Use(RequireWebauthnInternalToken(webauthnInternalToken))
	{
		webauthnInternal.GET("/:credential_id", h.Webauthn.Get)
		webauthnInternal.PATCH("/:credential_id/sign-count", h.Webauthn.UpdateSignCount)
	}

	v1 := router.Group("/internal/v1")
	v1.Use(authjwt.RequireAuth(verifier, logger))
	{
		v1.POST("/users/provision", h.User.Provision)
		v1.GET("/users", h.User.List)
		v1.PATCH("/users/:id/role", h.User.UpdateRole)

		v1.POST("/auth/webauthn/credentials", h.Webauthn.Register)

		v1.GET("/tasks", h.Task.List)
		v1.POST("/tasks", h.Task.Create)
		v1.GET("/tasks/:id", h.Task.Get)
		v1.PATCH("/tasks/:id", h.Task.Update)
		v1.DELETE("/tasks/:id", h.Task.Delete)

		v1.GET("/labels", h.Label.List)
		v1.POST("/labels", h.Label.Create)
		v1.PATCH("/labels/:id", h.Label.Update)
		v1.DELETE("/labels/:id", h.Label.Delete)
	}

	return router
}
