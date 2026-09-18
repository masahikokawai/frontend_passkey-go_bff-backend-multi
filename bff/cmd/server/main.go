// Command server はBFFのエントリポイント
// Rails版には存在しない層(BFFという「クライアント専用の窓口」)を、
// Go(Gin)で1プロセスとして起動する
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/masahikokawai/bff-gin/bff/internal/auth"
	"github.com/masahikokawai/bff-gin/bff/internal/config"
	"github.com/masahikokawai/bff-gin/bff/internal/featureflag"
	"github.com/masahikokawai/bff-gin/bff/internal/proxy"
)

func main() {
	// configロード前はまだLOG_LEVELが読めないため、既定レベル(Info)の
	// bootLoggerで起動時エラーだけを出す(training-go/gin(参照元)と同じ構成)
	bootLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		bootLogger.Error("設定の読み込みに失敗しました", "error", err)
		os.Exit(1)
	}

	// configロード後は cfg.LogLevel(LOG_LEVEL環境変数)に従ったレベルへ切り替える
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.SlogLevel()}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, DB: cfg.RedisDB})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error("Redisへの接続に失敗しました", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	oidcClient, err := auth.NewOIDCClient(ctx, cfg.OIDCIssuerURL, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.OIDCRedirectURL, redisClient)
	if err != nil {
		logger.Error("OIDCクライアントの初期化に失敗しました", "error", err)
		os.Exit(1)
	}

	flagEvaluator, err := featureflag.NewEvaluator(ctx, cfg.FeatureFlagExportURL, cfg.FeatureFlagPollToken, logger)
	if err != nil {
		logger.Error("Feature Flag providerの初期化に失敗しました", "error", err)
		os.Exit(1)
	}

	sessionStore := auth.NewStore(redisClient)
	refresher := auth.NewRefresher(sessionStore, oidcClient)

	cookieCfg := auth.CookieConfig{
		SessionCookieName: cfg.SessionCookieName,
		CSRFCookieName:    cfg.CSRFCookieName,
		Secure:            !cfg.IsDevelopment(),
	}

	taskClientV1 := proxy.NewTaskClientV1(cfg.BackendRESTBaseURL)
	taskClientV2, closeGRPC, err := proxy.NewTaskClientV2(cfg.BackendGRPCAddr)
	if err != nil {
		logger.Error("backend v2(gRPC)クライアントの初期化に失敗しました", "error", err)
		os.Exit(1)
	}
	defer closeGRPC()

	// CONTRACT.mdセクション20: go以外の言語実装も同じTaskClientV1/V2(ワイヤー契約が
	// 同一のため新規クライアントコードは不要)を、それぞれの接続先でインスタンス化するだけでよい
	rustClientV1 := proxy.NewTaskClientV1(cfg.RustRESTBaseURL)
	rustClientV2, closeRustGRPC, err := proxy.NewTaskClientV2(cfg.RustGRPCAddr)
	if err != nil {
		logger.Error("backend-rust v2(gRPC)クライアントの初期化に失敗しました", "error", err)
		os.Exit(1)
	}
	defer closeRustGRPC()

	scalaHTTP4sClientV1 := proxy.NewTaskClientV1(cfg.ScalaHTTP4sRESTBaseURL)
	scalaHTTP4sClientV2, closeScalaHTTP4sGRPC, err := proxy.NewTaskClientV2(cfg.ScalaHTTP4sGRPCAddr)
	if err != nil {
		logger.Error("backend-scala-http4s v2(gRPC)クライアントの初期化に失敗しました", "error", err)
		os.Exit(1)
	}
	defer closeScalaHTTP4sGRPC()

	scalaPekkoClientV1 := proxy.NewTaskClientV1(cfg.ScalaPekkoRESTBaseURL)
	scalaPekkoClientV2, closeScalaPekkoGRPC, err := proxy.NewTaskClientV2(cfg.ScalaPekkoGRPCAddr)
	if err != nil {
		logger.Error("backend-scala-pekko v2(gRPC)クライアントの初期化に失敗しました", "error", err)
		os.Exit(1)
	}
	defer closeScalaPekkoGRPC()

	railsClientV1 := proxy.NewTaskClientV1(cfg.RailsRESTBaseURL)
	railsClientV2, closeRailsGRPC, err := proxy.NewTaskClientV2(cfg.RailsGRPCAddr)
	if err != nil {
		logger.Error("backend-rails v2(gRPC)クライアントの初期化に失敗しました", "error", err)
		os.Exit(1)
	}
	defer closeRailsGRPC()

	jsClientV1 := proxy.NewTaskClientV1(cfg.JSRESTBaseURL)
	jsClientV2, closeJSGRPC, err := proxy.NewTaskClientV2(cfg.JSGRPCAddr)
	if err != nil {
		logger.Error("backend-js-express v2(gRPC)クライアントの初期化に失敗しました", "error", err)
		os.Exit(1)
	}
	defer closeJSGRPC()

	tsClientV1 := proxy.NewTaskClientV1(cfg.TSRESTBaseURL)
	tsClientV2, closeTSGRPC, err := proxy.NewTaskClientV2(cfg.TSGRPCAddr)
	if err != nil {
		logger.Error("backend-js-ts-express v2(gRPC)クライアントの初期化に失敗しました", "error", err)
		os.Exit(1)
	}
	defer closeTSGRPC()

	labelClient := proxy.NewLabelClientV1(cfg.BackendRESTBaseURL)
	provisionClient := proxy.NewUserProvisionClient(cfg.BackendRESTBaseURL)

	localLoginClient := auth.NewLocalLoginClient(cfg.LocalAuthVerifyPasswordURL, cfg.LocalAuthInternalToken)
	localRSAKeys, err := auth.NewLocalRSAKeyPair()
	if err != nil {
		logger.Error("ローカル認証(RSA)の鍵ペア生成に失敗しました", "error", err)
		os.Exit(1)
	}

	// CONTRACT.mdセクション22: パスキー(WebAuthn)、bffのローカル認証ユーザーへの
	// 追加の認証手段のみが対象
	webAuthn, err := auth.NewWebAuthn(cfg.WebauthnRPID, cfg.WebauthnRPDisplayName, cfg.WebauthnRPOrigin)
	if err != nil {
		logger.Error("WebAuthnの初期化に失敗しました", "error", err)
		os.Exit(1)
	}
	webauthnChallengeStore := auth.NewWebauthnChallengeStore(redisClient)
	webauthnBackendClient := auth.NewWebauthnBackendClient(cfg.BackendRESTBaseURL, cfg.WebauthnInternalToken)

	authHandler := &auth.Handler{
		OIDC:                  oidcClient,
		Store:                 sessionStore,
		Provisioner:           provisionClient,
		Flags:                 flagEvaluator,
		CookieCfg:             cookieCfg,
		PostLogoutRedirectURL: cfg.PostLogoutRedirect,
		FrontendBaseURL:       cfg.FrontendBaseURL,
		LocalLogin:            localLoginClient,
		HMACSecret:            cfg.LocalAuthHMACSecret,
		LocalRSAKeys:          localRSAKeys,
		WebAuthn:              webAuthn,
		WebauthnChallenge:     webauthnChallengeStore,
		WebauthnBackend:       webauthnBackendClient,
		Logger:                logger,
	}
	// CONTRACT.mdセクション20: 言語(backend.task-language)ごとにキーを分けたmapで保持する
	// go/rust/scala-http4s/scala-pekko/railsの5言語×2プロトコル(rest/grpc)、計10エントリ
	// いずれもTaskClientV1/V2(既存のGo実装用クライアント)をそのまま異なる接続先で
	// インスタンス化しただけで、新規のクライアントコードは書いていない
	// (ワイヤー契約パリティ(セクション20.5)を各言語実装が満たしているため成立する)
	taskRoutes := &proxy.TaskRoutes{
		Clients: map[string]proxy.TaskBackendClient{
			"go:rest":           taskClientV1,
			"go:grpc":           taskClientV2,
			"rust:rest":         rustClientV1,
			"rust:grpc":         rustClientV2,
			"scala-http4s:rest": scalaHTTP4sClientV1,
			"scala-http4s:grpc": scalaHTTP4sClientV2,
			"scala-pekko:rest":  scalaPekkoClientV1,
			"scala-pekko:grpc":  scalaPekkoClientV2,
			"rails:rest":        railsClientV1,
			"rails:grpc":        railsClientV2,
			"javascript:rest":   jsClientV1,
			"javascript:grpc":   jsClientV2,
			"typescript:rest":   tsClientV1,
			"typescript:grpc":   tsClientV2,
		},
		Flags:     flagEvaluator,
		Refresher: refresher,
		Logger:    logger,
	}
	labelRoutes := &proxy.LabelRoutes{Client: labelClient, Refresher: refresher}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger(logger))
	router.Use(auth.SecurityHeadersMiddleware())
	router.Use(auth.MaxBodySizeMiddleware())
	router.Use(auth.CSRFMiddleware(cookieCfg))

	// /.well-known/jwks.json はローカル認証(RSA版)の検証用にbffが公開鍵を配布するエンドポイント(CONTRACT.mdセクション16.4)
	// 認証不要のため /api 配下ではなくルート直下に置く(一般的なJWKS配布の慣例に合わせる)
	router.GET("/.well-known/jwks.json", authHandler.JWKS)

	api := router.Group("/api")
	{
		api.GET("/auth/login/keycloak", authHandler.LoginKeycloak)
		api.GET("/auth/callback", authHandler.Callback)
		api.POST("/auth/login", authHandler.LoginLocal)
		api.POST("/auth/login/rsa", authHandler.LoginLocalRSA)
		// CONTRACT.mdセクション22: ログイン試行中はまだセッションが無いため公開ルート
		api.POST("/auth/passkey/login/begin", authHandler.WebauthnLoginBegin)
		api.POST("/auth/passkey/login/finish", authHandler.WebauthnLoginFinish)

		authorized := api.Group("/")
		authorized.Use(auth.RequireSession(sessionStore, cookieCfg))
		{
			authorized.POST("/auth/logout", authHandler.Logout)
			authorized.GET("/me", authHandler.Me)
			authorized.POST("/auth/passkey/register/begin", authHandler.WebauthnRegisterBegin)
			authorized.POST("/auth/passkey/register/finish", authHandler.WebauthnRegisterFinish)
			authorized.GET("/tasks", taskRoutes.List)
			authorized.POST("/tasks", taskRoutes.Create)
			authorized.GET("/tasks/:id", taskRoutes.Get)
			authorized.PATCH("/tasks/:id", taskRoutes.Update)
			authorized.DELETE("/tasks/:id", taskRoutes.Delete)
			authorized.GET("/labels", labelRoutes.List)
			authorized.POST("/labels", labelRoutes.Create)
			authorized.PATCH("/labels/:id", labelRoutes.Update)
			authorized.DELETE("/labels/:id", labelRoutes.Delete)
		}
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("BFFサーバーを起動します", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("サーバーの起動に失敗しました", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("シャットダウンを開始します")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdownに失敗しました", "error", err)
	}
}

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}
