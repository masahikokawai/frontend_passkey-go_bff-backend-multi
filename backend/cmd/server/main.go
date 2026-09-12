// backendのエントリポイント
// REST v1(Gin)とgRPC v2を同一プロセス内・別ポートで起動する
//
// Rails対比: Railsは `rails server` 1コマンド・1ポートだが、このbackendは
// 「同じドメインロジックを異なるプロトコルで公開する」学習目的のため2ポート持つ
// 本番構成であれば通常はどちらか一方(または別プロセス)に整理する
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/config"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/featureflag"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/grpcserver"
	external "github.com/masahikokawai/training-go/bff-gin/backend/internal/handler/external"
	v1 "github.com/masahikokawai/training-go/bff-gin/backend/internal/handler/v1"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func main() {
	// configロード前はまだLOG_LEVELが読めないため、既定レベル(Info)の
	// bootLoggerで起動時エラーだけを出す(training-go/gin(参照元)と同じ構成)
	bootLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		bootLogger.Error("設定読み込み失敗", slog.Any("error", err))
		os.Exit(1)
	}

	// configロード後は cfg.LogLevel(LOG_LEVEL環境変数)に従ったレベルへ切り替える
	// LOG_LEVEL=debug にするとGORMが発行したSQLもログに出る(internal/db/logger.go参照)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.SlogLevel()}))

	gormDB, err := db.New(cfg.DBDSN, logger)
	if err != nil {
		logger.Error("DB接続失敗", slog.Any("error", err))
		os.Exit(1)
	}

	verifier := authjwt.NewVerifier(cfg.KeycloakJWKSURL, cfg.KeycloakIssuer, cfg.ExpectedAudience)
	if err := verifier.Prefetch(); err != nil {
		// 起動直後にKeycloakがまだ準備できていないケースもあるため、失敗しても
		// 致命的エラーにはしない(初回リクエスト時にkeyFunc経由で再取得される)
		logger.Warn("JWKSの事前取得に失敗(初回リクエスト時に再試行する)", slog.Any("error", err))
	}

	// ローカル(非Keycloak)認証の3方式めのverifier(CONTRACT.mdセクション16.5)
	// bffが公開するJWKS(RSA版)は、既存のKeycloak向けVerifierと全く同じ実装
	// (jwksURL/issuer/audienceの汎用実装)を別issuer/別URLで1つ増やすだけで良い
	localRSAVerifier := authjwt.NewVerifier(cfg.LocalRSAJWKSURL, authjwt.LocalRSAIssuer, cfg.ExpectedAudience)
	if err := localRSAVerifier.Prefetch(); err != nil {
		// bffがまだ起動していないケースもあるため、Keycloak向けと同様に致命的エラーにはしない
		logger.Warn("ローカルRSA JWKSの事前取得に失敗(初回リクエスト時に再試行する)", slog.Any("error", err))
	}
	localHMACVerifier := authjwt.NewHMACVerifier(cfg.LocalHMACSecret, authjwt.LocalHMACIssuer, cfg.ExpectedAudience)

	// Dispatcher:
	//   JWTの`iss`クレームで Keycloak/ローカルHMAC/ローカルRSA の3方式を振り分ける(CONTRACT.mdセクション16.5)
	// RequireAuth/RequireExternalClientAuth/
	// gRPC interceptor のいずれもこの Dispatcher を共通の TokenVerifier として使う
	dispatcher := authjwt.NewDispatcher().
		Register(cfg.KeycloakIssuer, verifier).
		Register(authjwt.LocalHMACIssuer, localHMACVerifier).
		Register(authjwt.LocalRSAIssuer, localRSAVerifier)

	userRepo := repository.NewUser(gormDB)
	taskRepo := repository.NewTask(gormDB)
	labelRepo := repository.NewLabel(gormDB)
	featureFlagRepo := repository.NewFeatureFlag(gormDB)
	webauthnCredentialRepo := repository.NewWebauthnCredential(gormDB)

	taskService := service.NewTaskService(taskRepo)
	userService := service.NewUserService(userRepo).WithPasskeyChecker(webauthnCredentialRepo)
	labelService := service.NewLabelService(labelRepo)
	localAuthService := service.NewLocalAuthService(userRepo)
	webauthnService := service.NewWebauthnService(webauthnCredentialRepo)

	v1.SetLogger(logger)
	restHandlers := v1.Handlers{
		Task:        v1.NewTaskHandler(taskService, userRepo),
		Label:       v1.NewLabelHandler(labelService),
		User:        v1.NewUserHandler(userService),
		FeatureFlag: v1.NewFeatureFlagExportHandler(featureFlagRepo),
		LocalAuth:   v1.NewLocalAuthHandler(localAuthService),
		Webauthn:    v1.NewWebauthnHandler(webauthnService, userRepo),
	}
	restRouter := v1.NewRouter(restHandlers, dispatcher, logger, cfg.FeatureFlagPollToken, cfg.LocalAuthInternalToken, cfg.AdminInternalToken, cfg.WebauthnInternalToken)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           restRouter,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// 外部公開API(CONTRACT.mdセクション11)
	// BFF を経由しないため Feature Flag は backend 自身が評価する
	// 【CONTRACT.mdセクション13で変更】以前はファイル(cfg.ExternalFeatureFlagPath)を読んでいたが、
	// admin/go・admin/railsからの変更を再デプロイ無しで反映できるよう
	// MySQL の feature_flags テーブルを正本にする NewMySQLEvaluator へ切り替えた
	extFlags, err := featureflag.NewMySQLEvaluator(context.Background(), featureFlagRepo, cfg.FeatureFlagPollInterval, logger)
	if err != nil {
		logger.Error("外部API用Feature Flagの初期化失敗", slog.Any("error", err))
		os.Exit(1)
	}
	externalHandlers := external.Handlers{
		Task: external.NewTaskHandler(taskService, extFlags, logger),
	}
	externalRouter := external.NewRouter(externalHandlers, dispatcher, cfg.ExternalAPIClientID, logger)
	externalHTTPServer := &http.Server{
		Addr:              cfg.ExternalHTTPAddr,
		Handler:           externalRouter,
		ReadHeaderTimeout: 5 * time.Second,
	}

	taskServer := grpcserver.NewTaskServer(taskService, userRepo)
	grpcSrv := grpcserver.New(dispatcher, taskServer, logger)
	grpcListener, err := grpcserver.Listen(grpcSrv, cfg.GRPCAddr)
	if err != nil {
		logger.Error("gRPC listen失敗", slog.Any("error", err))
		os.Exit(1)
	}

	go func() {
		logger.Info("REST v1(Gin)起動", slog.String("addr", cfg.HTTPAddr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("REST v1サーバーエラー", slog.Any("error", err))
		}
	}()
	go func() {
		logger.Info("gRPC v2起動", slog.String("addr", cfg.GRPCAddr))
		if err := grpcSrv.Serve(grpcListener); err != nil {
			logger.Error("gRPC v2サーバーエラー", slog.Any("error", err))
		}
	}()
	go func() {
		logger.Info("外部公開API起動", slog.String("addr", cfg.ExternalHTTPAddr))
		if err := externalHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("外部公開APIサーバーエラー", slog.Any("error", err))
		}
	}()

	// graceful shutdown
	// Rails(Puma)のSIGTERMハンドリングに相当する
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("シャットダウン開始")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("REST v1のシャットダウンに失敗", slog.Any("error", err))
	}
	if err := externalHTTPServer.Shutdown(ctx); err != nil {
		logger.Error("外部公開APIのシャットダウンに失敗", slog.Any("error", err))
	}
	grpcSrv.GracefulStop()
	logger.Info("シャットダウン完了")
}
