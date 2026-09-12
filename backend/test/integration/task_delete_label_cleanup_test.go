//go:build integration

// 3回目のテスト監査で発覚した実バグの回帰防止テスト:
// TaskService.Delete(repository.Task.Delete)がtask_labelsの関連行を削除しておらず、
// タスク削除後もtask_labels側に孤立行が残っていた(label_delete_integrity_test.goで
// ラベル削除の後始末を書いた際に発覚した、Task削除側の対になるバグ)
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

func TestDeleteTask_CleansUpTaskLabels(t *testing.T) {
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
	ctx := context.Background()
	userRepo := repository.NewUser(gormDB)
	taskRepo := repository.NewTask(gormDB)
	labelRepo := repository.NewLabel(gormDB)
	taskService := service.NewTaskService(taskRepo)
	labelService := service.NewLabelService(labelRepo)

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-task-delete-cleanup", "task-delete-cleanup@example.com", "整合性三郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	var prevTaskIDs []uint64
	gormDB.Model(&model.Task{}).Where("user_id = ?", user.ID).Pluck("id", &prevTaskIDs)
	if len(prevTaskIDs) > 0 {
		gormDB.Exec("DELETE FROM task_labels WHERE task_id IN ?", prevTaskIDs)
	}
	gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{})
	gormDB.Unscoped().Where("name = ?", "削除後始末確認").Delete(&model.Label{})

	label, err := labelService.Create(ctx, "削除後始末確認")
	if err != nil {
		t.Fatalf("テスト用ラベル作成失敗: %v", err)
	}
	task, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name:       "削除後始末確認用",
		Status:     "waiting",
		FinishedOn: time.Now().AddDate(0, 0, 7),
		LabelIDs:   []uint64{label.ID},
	})
	if err != nil {
		t.Fatalf("テスト用タスク作成失敗: %v", err)
	}

	if err := taskService.Delete(ctx, task.ID, user.ID); err != nil {
		t.Fatalf("タスク削除失敗: %v", err)
	}

	// 【本題】タスク削除後、task_labelsに孤立行が残っていないこと
	var count int64
	if err := gormDB.Table("task_labels").Where("task_id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatalf("task_labelsの確認に失敗: %v", err)
	}
	if count != 0 {
		t.Errorf("タスク削除後もtask_labelsに%d件の孤立行が残っている(修正前の挙動に戻っている)", count)
	}

	// 副次確認: task_labelsが片付いたことで、ラベル自体は(他に使われていなければ)削除できる
	if err := labelService.Delete(ctx, label.ID); err != nil {
		t.Errorf("後始末後もラベル削除に失敗: %v", err)
	}
}

// 【6回目のテスト監査(DELETE冪等性のクロス言語パリティ角度)で追加】
// 既に削除済みのtask idへ再度Deleteを呼んでも、クラッシュ(生のDBエラー等)せず
// 一貫してErrNotFoundになることを確認する。repository.Task.Deleteが
// RowsAffected==0をgorm.ErrRecordNotFoundとして返す実装(task.go参照)の回帰確認。
// Rust/Scala(http4s)/Scala(Pekko)/Railsとも同じ「rows affected / find_by」方式で
// 同一の挙動(2回目は404相当)になることを別途確認済み(CONTRACT.mdセクション23.1参照)
func TestDeleteTask_二回目の削除はErrNotFoundになる(t *testing.T) {
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
	ctx := context.Background()
	userRepo := repository.NewUser(gormDB)
	taskRepo := repository.NewTask(gormDB)
	taskService := service.NewTaskService(taskRepo)

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-task-double-delete", "task-double-delete@example.com", "二重削除次郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{})

	task, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name:       "二重削除確認用",
		Status:     "waiting",
		FinishedOn: time.Now().AddDate(0, 0, 7),
	})
	if err != nil {
		t.Fatalf("テスト用タスク作成失敗: %v", err)
	}

	if err := taskService.Delete(ctx, task.ID, user.ID); err != nil {
		t.Fatalf("1回目の削除に失敗: %v", err)
	}

	err = taskService.Delete(ctx, task.ID, user.ID)
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("2回目の削除のerror = %v, service.ErrNotFoundを期待(生のDBエラーが伝播していないか確認)", err)
	}
}
