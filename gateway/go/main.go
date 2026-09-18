package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	resolver, err := NewLanguageResolver(ctx, cfg.FeatureFlagExportURL, cfg.FeatureFlagPollToken, cfg.pollInterval(), logger)
	if err != nil {
		logger.Error("backend.task-language resolverの初期化に失敗しました", "error", err)
		os.Exit(1)
	}

	gw := &Gateway{
		Targets: map[string]string{
			// CONTRACT.mdセクション20.7: 5言語すべてが外部公開APIを実装済み
			"go":           cfg.GoExternalBaseURL,
			"rust":         cfg.RustExternalBaseURL,
			"scala-http4s": cfg.ScalaHTTP4sExternalBaseURL,
			"scala-pekko":  cfg.ScalaPekkoExternalBaseURL,
			"rails":        cfg.RailsExternalBaseURL,
			"javascript":   cfg.JSExternalBaseURL,
			"typescript":   cfg.TSExternalBaseURL,
		},
		ResolveLanguage: func() string { return resolver.Resolve(ctx) },
		Logger:          logger,
		AllowedOrigin:   cfg.AllowedOrigin,
	}

	srv := &http.Server{
		Addr:              cfg.GatewayAddr,
		Handler:           gw.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("外部公開APIゲートウェイ(Go製)を起動します", "addr", cfg.GatewayAddr)
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
