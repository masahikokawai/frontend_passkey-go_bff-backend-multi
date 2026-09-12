// admin-go のエントリポイント
// Feature Flag(feature_flags/feature_flag_audit_logs)を
// MySQLへ直接読み書きする、独立した小さな管理画面(CONTRACT.mdセクション13)
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/client"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/config"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/handler"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.Load()

	gormDB, err := db.New(cfg.DBDSN)
	if err != nil {
		logger.Error("DB接続失敗", slog.Any("error", err))
		os.Exit(1)
	}

	handler.LoadTemplates("web/templates/*.html")

	repo := repository.NewFeatureFlag(gormDB)
	svc := service.NewFeatureFlagService(repo)
	h := handler.NewFeatureFlagHandler(svc)

	userClient := client.NewUserClient(cfg.BackendInternalBaseURL, cfg.AdminInternalToken)
	uh := handler.NewUserHandler(userClient)

	router := handler.NewRouter(h, uh, cfg.BasicAuthUser, cfg.BasicAuthPassword)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("admin-go起動", slog.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("サーバー起動失敗", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("シャットダウン開始")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdownに失敗しました", slog.Any("error", err))
	}
}
