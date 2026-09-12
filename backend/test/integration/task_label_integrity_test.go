//go:build integration

// 2回目のテスト監査で発覚した実バグの再現・回帰防止テスト:
// task_labelsテーブルにlabel_id/task_idへの外部キー制約が無く(migrations/000004参照)、
// TaskService.Create/Updateも存在しないlabel_idを検証していなかったため、
// 存在しないlabel_idを指定してもエラーにならず、task_labelsに永久に孤立する行が
// 作られてしまっていた(実際に統合テストで再現し、DBの中身を直接確認して発覚した)
// service.TaskService.validateLabelIDsExist(repository.FindMissingLabelIDsを使う)で修正済み
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

func TestCreateTask_WithNonexistentLabelID_ReturnsValidationError(t *testing.T) {
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
	taskService := service.NewTaskService(taskRepo)
	ctx := context.Background()

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-label-integrity", "label-integrity@example.com", "整合性太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	// task_v1_v2_compare_test.goと同じ理由: 再実行しても結果が変わらないよう、
	// 前回実行分の後始末を先に行う(このテストは失敗するはずのCreateなので通常は
	// tasks/task_labelsに何も残らないが、念のため揃える)
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクの後始末に失敗: %v", err)
	}

	const nonexistentLabelID = uint64(999999999)
	_, err = taskService.Create(ctx, user.ID, service.TaskInput{
		Name:       "存在しないラベルを指定",
		Status:     "waiting",
		FinishedOn: time.Now().AddDate(0, 0, 7),
		LabelIDs:   []uint64{nonexistentLabelID},
	})
	if err == nil {
		t.Fatal("存在しないlabel_idを指定してもエラーにならなかった(修正前の挙動に戻っている)")
	}
	if !errors.Is(err, service.ErrValidation) {
		t.Errorf("ErrValidationでラップされていない: %v", err)
	}

	// 【本題】孤立したtask_labels行が作られていないことも直接確認する
	// (修正前は、Createがエラーを返さないままtask_labelsにだけ孤立行が残っていた)
	var count int64
	if err := gormDB.Table("task_labels").Where("label_id = ?", nonexistentLabelID).Count(&count).Error; err != nil {
		t.Fatalf("task_labelsの確認に失敗: %v", err)
	}
	if count != 0 {
		t.Errorf("存在しないlabel_idを指すtask_labels行が%d件残っている(孤立データが作られた)", count)
	}
}

// TestCreateTask_WithExistingLabelIDs_Succeeds は上記の検証追加が、既存の正常系
// (実在するlabel_idを指定するケース)を壊していないことを確認する回帰テスト
func TestCreateTask_WithExistingLabelIDs_Succeeds(t *testing.T) {
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

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-label-integrity-ok", "label-integrity-ok@example.com", "整合性花子", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	// 【再実行しても同じ結果になるように】前回実行分のタスク(とtask_labels)を先に消しておく
	var prevTaskIDs []uint64
	gormDB.Model(&model.Task{}).Where("user_id = ?", user.ID).Pluck("id", &prevTaskIDs)
	if len(prevTaskIDs) > 0 {
		if err := gormDB.Table("task_labels").Where("task_id IN ?", prevTaskIDs).Delete(nil).Error; err != nil {
			t.Fatalf("既存task_labelsの後始末に失敗: %v", err)
		}
	}
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクの後始末に失敗: %v", err)
	}
	labels, err := labelRepo.List(ctx)
	if err != nil || len(labels) == 0 {
		t.Fatalf("labelsが1件以上必要(migrations/000005のシード前提): got=%d err=%v", len(labels), err)
	}

	created, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name:       "実在するラベルを指定",
		Status:     "waiting",
		FinishedOn: time.Now().AddDate(0, 0, 7),
		LabelIDs:   []uint64{labels[0].ID},
	})
	if err != nil {
		t.Fatalf("実在するlabel_idを指定してもエラーになった(検証が厳しすぎる回帰): %v", err)
	}
	if len(created.Labels) != 1 || created.Labels[0].ID != labels[0].ID {
		t.Errorf("Labels = %+v, want [%d]", created.Labels, labels[0].ID)
	}
}
