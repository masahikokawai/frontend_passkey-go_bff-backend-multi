//go:build integration

// 3回目のテスト監査(認可・データ整合性観点)で発覚した実バグの再現・回帰防止テスト:
// task_labelsテーブルにFK制約が無いため、タスクに紐付いたままラベルを削除できてしまい、
// task_labels側に「存在しないlabel_idを指す」孤立行が残っていた(TestCreateTask_WithNonexistentLabelIDと
// 対になる、作成時ではなく削除時の孤立化パターン)
// service.LabelService.Delete(repository.Label.CountTaskLabelsを使う)で修正済み
package integration

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestDeleteLabel_InUse_ReturnsValidationErrorAndDoesNotOrphanTaskLabels(t *testing.T) {
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
	labelService := service.NewLabelService(labelRepo)
	ctx := context.Background()

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-label-delete-integrity", "label-delete-integrity@example.com", "整合性次郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	// 再実行しても結果が変わらないよう、前回実行分の後始末を先に行う
	var prevTaskIDs []uint64
	gormDB.Model(&model.Task{}).Where("user_id = ?", user.ID).Pluck("id", &prevTaskIDs)
	if len(prevTaskIDs) > 0 {
		gormDB.Exec("DELETE FROM task_labels WHERE task_id IN ?", prevTaskIDs)
	}
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクの後始末に失敗: %v", err)
	}
	gormDB.Unscoped().Where("name = ?", "整合性確認ラベル").Delete(&model.Label{})

	label, err := labelService.Create(ctx, "整合性確認ラベル")
	if err != nil {
		t.Fatalf("テスト用ラベル作成失敗: %v", err)
	}

	task, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name:       "ラベル削除確認用",
		Status:     "waiting",
		FinishedOn: time.Now().AddDate(0, 0, 7),
		LabelIDs:   []uint64{label.ID},
	})
	if err != nil {
		t.Fatalf("テスト用タスク作成失敗: %v", err)
	}

	// 【本題】タスクに紐付いたままの削除は拒否される
	err = labelService.Delete(ctx, label.ID)
	if err == nil {
		t.Fatal("使用中のラベル削除がエラーにならなかった(修正前の挙動に戻っている)")
	}
	if !errors.Is(err, service.ErrValidation) {
		t.Errorf("ErrValidationでラップされていない: %v", err)
	}

	// ラベル自体がまだ存在すること、task_labelsの関連も消えていないことを直接確認する
	var labelCount int64
	if err := gormDB.Model(&model.Label{}).Where("id = ?", label.ID).Count(&labelCount).Error; err != nil {
		t.Fatalf("labelsの確認に失敗: %v", err)
	}
	if labelCount != 1 {
		t.Errorf("拒否されたはずなのにラベルが削除されている(labelCount=%d)", labelCount)
	}
	var taskLabelCount int64
	if err := gormDB.Table("task_labels").Where("label_id = ? AND task_id = ?", label.ID, task.ID).Count(&taskLabelCount).Error; err != nil {
		t.Fatalf("task_labelsの確認に失敗: %v", err)
	}
	if taskLabelCount != 1 {
		t.Errorf("task_labelsの関連が消えている(taskLabelCount=%d)", taskLabelCount)
	}

	// 後始末: タスクを消してからならラベル削除が成功することも確認する(回帰確認を兼ねる)
	if err := taskService.Delete(ctx, task.ID, user.ID); err != nil {
		t.Fatalf("後始末のタスク削除に失敗: %v", err)
	}
	if err := labelService.Delete(ctx, label.ID); err != nil {
		t.Errorf("未使用になった後もラベル削除が失敗する: %v", err)
	}
}
