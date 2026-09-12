//go:build integration

// 外部公開API(CONTRACT.mdセクション11)のoffsetページング(v1)/keyset・cursorページング(v2)が
// 同じデータ集合に対して正しくページ送りできることを実MySQLで確認する結合テスト
// 実行前提: 環境変数 TEST_DB_DSN(README参照)
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

func TestExternalPagination_OffsetAndCursorCoverSameTasks(t *testing.T) {
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

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-external-pagination", "external-pagination@example.com", "外部API太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}

	// 【実機検証で判明した不具合の修正】このテストは固定のkeycloak_subを使い回すため、
	// 既存タスクを削除せずに実行すると、2回目以降の実行でtotalが前回分と合算されて失敗する(非冪等だった)
	// 実行のたびにこのユーザーのタスクを空の状態から始められるよう、作成前に既存分を削除しておく
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクのクリーンアップ失敗: %v", err)
	}

	finishedOn := time.Now().AddDate(0, 0, 7)
	const total = 25
	for i := 0; i < total; i++ {
		if _, err := taskService.Create(ctx, user.ID, service.TaskInput{
			Name:       "外部APIタスク",
			Status:     "waiting",
			FinishedOn: finishedOn,
		}); err != nil {
			t.Fatalf("タスク作成失敗(i=%d): %v", i, err)
		}
	}

	// v1: offsetページング
	// page_size=10で全件を数えられることを確認する
	seenIDsV1 := map[uint64]bool{}
	for page := 1; ; page++ {
		dtos, gotTotal, err := taskService.ListExternalV1(ctx, service.ExternalListFilterV1{
			UserID: user.ID, Page: page, PageSize: 10,
		})
		if err != nil {
			t.Fatalf("ListExternalV1失敗(page=%d): %v", page, err)
		}
		if int(gotTotal) != total {
			t.Fatalf("total = %d, want %d", gotTotal, total)
		}
		if len(dtos) == 0 {
			break
		}
		for _, d := range dtos {
			seenIDsV1[d.ID] = true
		}
		if page > 10 {
			t.Fatal("ページングが終端しない(無限ループの疑い)")
		}
	}
	if len(seenIDsV1) != total {
		t.Errorf("v1で収集できたタスク数 = %d, want %d", len(seenIDsV1), total)
	}

	// v2: keyset(cursor)ページング
	// 同じ全件数をcursorで辿れることを確認する
	seenIDsV2 := map[uint64]bool{}
	cursor := ""
	for i := 0; ; i++ {
		dtos, next, err := taskService.ListExternalV2(ctx, service.ExternalListFilterV2{
			UserID: user.ID, Cursor: cursor, Limit: 10,
		})
		if err != nil {
			t.Fatalf("ListExternalV2失敗(i=%d): %v", i, err)
		}
		for _, d := range dtos {
			seenIDsV2[d.ID] = true
		}
		if next == "" {
			break
		}
		cursor = next
		if i > 10 {
			t.Fatal("cursorページングが終端しない(無限ループの疑い)")
		}
	}
	if len(seenIDsV2) != total {
		t.Errorf("v2で収集できたタスク数 = %d, want %d", len(seenIDsV2), total)
	}

	for id := range seenIDsV1 {
		if !seenIDsV2[id] {
			t.Errorf("v1で見えたtask id=%d がv2では見えない", id)
		}
	}
}
