//go:build integration

// テスト監査(9回目、重複label_idsという角度)で追加:
// task_labelsには(task_id, label_id)のUNIQUE KEY(migrations/000004)があるため、
// label_ids:[3,3,5]のように同じlabel_idを複数回渡した場合、GORMのAssociation.Replaceが
// 内部でどう振る舞うか(重複行を1つにまとめて成功するのか、UNIQUE制約違反の生エラーに
// なるのか)は自明ではなく、実際に確認するまで未検証だった
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

func TestCreateTask_WithDuplicateLabelID_DedupesCleanly(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ(README参照)")
	}

	gormDB, err := db.New(dsn, slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}
	// 【テスト基盤監査で追加】未Close()だとintegrationパッケージ内の逐次実行中に
	// コネクションプールが積み上がる(task_nul_byte_test.goの同名コメント参照)
	if sqlDB, sqlErr := gormDB.DB(); sqlErr == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}

	userRepo := repository.NewUser(gormDB)
	taskRepo := repository.NewTask(gormDB)
	labelRepo := repository.NewLabel(gormDB)
	taskService := service.NewTaskService(taskRepo)
	ctx := context.Background()

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-dup-label", "dup-label@example.com", "重複太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクの後始末に失敗: %v", err)
	}

	labels, err := labelRepo.List(ctx)
	if err != nil || len(labels) < 2 {
		t.Fatalf("labelsが2件以上必要(migrations/000005のシード前提): got=%d err=%v", len(labels), err)
	}
	dupID := labels[0].ID
	otherID := labels[1].ID

	finishedOn := time.Now().AddDate(0, 0, 7)
	created, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name:       "重複label_id作成テスト",
		Status:     "waiting",
		FinishedOn: finishedOn,
		// 同じlabel_id(dupID)を2回含める。UNIQUE KEY(task_id, label_id)があるため、
		// アプリ側で重複除去していなければここでUNIQUE制約違反(Error 1062)の生エラーになるはず
		LabelIDs: []uint64{dupID, dupID, otherID},
	})
	if err != nil {
		t.Fatalf("重複label_idを含むCreate()がエラーになった(重複除去できていない): %v", err)
	}

	var count int64
	if err := gormDB.Model(&model.TaskLabel{}).
		Where("task_id = ? AND label_id = ?", created.ID, dupID).
		Count(&count).Error; err != nil {
		t.Fatalf("task_labels件数取得失敗: %v", err)
	}
	if count != 1 {
		t.Errorf("重複したlabel_id(id=%d)のtask_labels行が%d件ある(1件に重複除去されるべき)", dupID, count)
	}

	var totalCount int64
	if err := gormDB.Model(&model.TaskLabel{}).Where("task_id = ?", created.ID).Count(&totalCount).Error; err != nil {
		t.Fatalf("task_labels合計件数取得失敗: %v", err)
	}
	if totalCount != 2 {
		t.Errorf("task_labels合計件数 = %d, want 2(dupID分1件+otherID分1件)", totalCount)
	}

	// Update経路でも同じことを確認する(Create/Updateで実装が分かれていないか確認する意味も込め)
	_, err = taskService.Update(ctx, created.ID, user.ID, service.TaskInput{
		Name:       "重複label_id更新テスト",
		Status:     "waiting",
		FinishedOn: finishedOn,
		LabelIDs:   []uint64{dupID, dupID},
	})
	if err != nil {
		t.Fatalf("重複label_idを含むUpdate()がエラーになった(重複除去できていない): %v", err)
	}
	if err := gormDB.Model(&model.TaskLabel{}).
		Where("task_id = ?", created.ID).
		Count(&totalCount).Error; err != nil {
		t.Fatalf("Update後のtask_labels件数取得失敗: %v", err)
	}
	if totalCount != 1 {
		t.Errorf("Update後のtask_labels件数 = %d, want 1(dupIDのみ重複除去されて1件)", totalCount)
	}
}
