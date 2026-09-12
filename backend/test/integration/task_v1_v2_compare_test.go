//go:build integration

// このファイルは実MySQLが必要な結合テスト(`task test:integration`相当)
// 通常の `go test ./...` では実行されず、`go test -tags=integration ./test/integration/...`
// のように明示した場合のみビルド・実行される(jinjer-ats方針: 単体/結合テストの分離)
//
// 目的: v1(N+1, ListLegacy)とv2(Preload, ListOptimized)が同じ入力データに対して
// 「同じ集合」を返すこと(実装は違っても結果は一致する)をgoogle/go-cmpで確認する
// これはCONTRACT.mdで定義した「N+1解消のビフォーアフター比較」学習教材の正しさを
// 保証するテストにあたる
//
// 実行前提: 環境変数 TEST_DB_DSN に疎通可能なMySQL DSNを設定しておくこと
package integration

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestListLegacyAndListOptimized_ReturnSameTasks(t *testing.T) {
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

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-compare", "compare@example.com", "比較太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}

	// 【再実行時に判明した不具合の修正】このテストは前回実行分のタスクを消さずに
	// 3件ずつ積み増していたため、蓄積件数がListOptimizedのLimit(20)を超えると
	// 「legacy total(制限なしの総数)とoptimized len(Limit=20で頭打ち)が一致しない」
	// という誤検知で失敗していた(task_external_pagination_test.goで既に修正済みのパターンと同じ原因)
	// テストは何度実行しても同じ結果になるべきなので、実行前にこのユーザーの既存タスクを消しておく
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクの後始末に失敗: %v", err)
	}

	finishedOn := time.Now().AddDate(0, 0, 7)
	names := []string{"タスクA", "タスクB", "タスクC"}
	for _, name := range names {
		_, err := taskService.Create(ctx, user.ID, service.TaskInput{
			Name:       name,
			Status:     "waiting",
			FinishedOn: finishedOn,
		})
		if err != nil {
			t.Fatalf("タスク作成失敗(%s): %v", name, err)
		}
	}

	legacyDTOs, legacyTotal, err := taskService.ListLegacy(ctx, user.ID, service.TaskListFilterV1{Limit: 20})
	if err != nil {
		t.Fatalf("ListLegacy失敗: %v", err)
	}
	optimizedDTOs, _, err := taskService.ListOptimized(ctx, user.ID, service.TaskListFilterV2{Limit: 20})
	if err != nil {
		t.Fatalf("ListOptimized失敗: %v", err)
	}

	if int64(len(optimizedDTOs)) != legacyTotal {
		t.Fatalf("件数不一致: legacy total=%d optimized len=%d", legacyTotal, len(optimizedDTOs))
	}

	sortByName := func(dtos []service.TaskDTO) []string {
		out := make([]string, 0, len(dtos))
		for _, d := range dtos {
			out = append(out, d.Name)
		}
		sort.Strings(out)
		return out
	}

	if diff := cmp.Diff(sortByName(legacyDTOs), sortByName(optimizedDTOs)); diff != "" {
		t.Errorf("v1(N+1)とv2(Preload)でタスク名集合が一致しない (-legacy +optimized):\n%s", diff)
	}
}
