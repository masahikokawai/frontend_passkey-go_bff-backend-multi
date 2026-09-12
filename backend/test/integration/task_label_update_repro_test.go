//go:build integration

// 統合レビューで報告された不具合の再現テスト:
// 「タスク編集でラベルを複数選択して更新すると500(internal_server_error)になる」
// 実行前提: 環境変数 TEST_DB_DSN(README参照)
// 既存のlabelsシード(migrations/000005)を前提にする
package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestUpdateTask_WithMultipleLabels_ReproducesInternalServerError(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ(README参照)")
	}

	gormDB, err := db.New(dsn, slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}
	// 【テスト監査で発見・修正】このDB接続をCloseせずにテストが終わっていたため、
	// 実行のたびに接続がリークしていた(internal/db/db.goのコメントで呼び出し側の責務と明記)
	if sqlDB, sqlErr := gormDB.DB(); sqlErr == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}

	userRepo := repository.NewUser(gormDB)
	taskRepo := repository.NewTask(gormDB)
	labelRepo := repository.NewLabel(gormDB)
	taskService := service.NewTaskService(taskRepo)
	ctx := context.Background()

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-label-repro", "label-repro@example.com", "ラベル太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}

	labels, err := labelRepo.List(ctx)
	if err != nil || len(labels) < 2 {
		t.Fatalf("labelsが2件以上必要(migrations/000005のシード前提): got=%d err=%v", len(labels), err)
	}
	labelIDs := []uint64{labels[0].ID, labels[1].ID}

	finishedOn := time.Now().AddDate(0, 0, 7)
	created, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name:       "ラベル更新再現用タスク",
		Status:     "waiting",
		FinishedOn: finishedOn,
		LabelIDs:   nil, // 作成時はラベル無し(報告内容の「編集で追加する時」を再現)
	})
	if err != nil {
		t.Fatalf("タスク作成失敗: %v", err)
	}

	// ここが報告された不具合の再現ポイント: 編集でラベルを複数選択して更新する
	_, err = taskService.Update(ctx, created.ID, user.ID, service.TaskInput{
		Name:       "ラベル更新再現用タスク",
		Status:     "waiting",
		FinishedOn: finishedOn,
		LabelIDs:   labelIDs,
	})
	if err != nil {
		t.Fatalf("再現: ラベル追加更新でエラーが発生した: %v", err)
	}
}
