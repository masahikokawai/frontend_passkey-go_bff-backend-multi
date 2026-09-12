//go:build integration

// タスク名・description に絵文字/サロゲートペア文字/HTMLタグに見える文字列を入れても、
// DBへの保存・取得で文字化けやエスケープ破壊が起きないことを確認する(エッジケース監査)
//
// XSS対策自体はfrontend側のJSXの自動エスケープに依存する設計であり(CONTRACT.md参照)、backend が文字列をHTMLエスケープする必要は無い
// ここで確認したいのは「backend が不用意にエスケープ/エンコードを壊していないか(素通しできているか)」の一点のみ
package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestTask_SpecialCharacters_RoundTripUnchanged(t *testing.T) {
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

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-special-chars", "special-chars@example.com", "特殊文字太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	// 前回実行分の後始末(非冪等バグの再発防止、既存の他統合テストと同じパターン)
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクの後始末に失敗: %v", err)
	}

	finishedOn := time.Now().AddDate(0, 0, 7)
	// タスク名は20文字以内の制約があるため短めの絵文字混じり文字列、
	// descriptionには文字数制限が無いため絵文字(サロゲートペア)・複合絵文字(ZWJ結合)・
	// HTMLタグに見える文字列をまとめて入れる
	name := "🎉タスク"
	description := "<script>alert(1)</script> 👨‍👩‍👧‍👦 絵文字混在 😀🔥 & \"quotes\" 'apostrophe'"

	created, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name:        name,
		Description: &description,
		Status:      "waiting",
		FinishedOn:  finishedOn,
	})
	if err != nil {
		t.Fatalf("特殊文字を含むタスクの作成に失敗: %v", err)
	}

	got, err := taskService.Get(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("作成したタスクの取得に失敗: %v", err)
	}

	if diff := cmp.Diff(name, got.Name); diff != "" {
		t.Errorf("nameが往復で変化した (-want +got):\n%s", diff)
	}
	if got.Description == nil {
		t.Fatal("descriptionがnilになった")
	}
	if diff := cmp.Diff(description, *got.Description); diff != "" {
		t.Errorf("descriptionが往復で変化した(エスケープ/エンコード破壊の疑い) (-want +got):\n%s", diff)
	}
}

// GET .../tasks?limit=0 のような境界値は、query paramのパース自体は通ってしまう
// (limit=-1のような負値はstrconv.ParseUintが失敗して既定値20にフォールバックするが、
// limit=0は正常にuint64としてパースできてしまうため、そのままrepositoryまで届く)
// GORMの Limit(0) は「上限なし」ではなく「0件に制限する」という、直感に反しやすい
// 挙動なので、実際にどうなるかをテストとして固定しておく(将来GORMのバージョンアップ等で
// この挙動が変わったら、このテストが検知する)
func TestTaskList_LimitZero_ReturnsEmptyNotDefault(t *testing.T) {
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

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-limit-zero", "limit-zero@example.com", "境界値太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクの後始末に失敗: %v", err)
	}

	finishedOn := time.Now().AddDate(0, 0, 7)
	if _, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name: "境界値確認用タスク", Status: "waiting", FinishedOn: finishedOn,
	}); err != nil {
		t.Fatalf("タスク作成失敗: %v", err)
	}

	dtos, total, err := taskService.ListLegacy(ctx, user.ID, service.TaskListFilterV1{Limit: 0, Offset: 0})
	if err != nil {
		t.Fatalf("ListLegacy(Limit:0)が失敗: %v", err)
	}
	if len(dtos) != 0 {
		t.Errorf("Limit:0のとき0件を期待したが len(dtos)=%d (GORMのLimit(0)挙動が変わった可能性)", len(dtos))
	}
	// totalはLimitと無関係に「絞り込み条件に一致する全件数」を返す設計なので1のまま
	if total != 1 {
		t.Errorf("total = %d, want 1 (Limitは件数自体を絞るだけで、total件数には影響しないはず)", total)
	}
}
