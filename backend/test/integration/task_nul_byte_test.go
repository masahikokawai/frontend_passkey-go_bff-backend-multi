//go:build integration

// タスク名・descriptionにNULバイト(\x00)や他のC0制御文字(\x01等)を含めても、
// GORM/bobいずれの経路でもDBへの保存・取得で切り詰め(strlen的なC文字列境界バグ)や
// 破損が起きないことを確認する(テスト監査で追加、task_special_characters_test.goが
// 絵文字/サロゲートペア/HTMLタグ風文字列はカバーしていたが、NULバイト単体は未カバーだった)
//
// go-sql-driver/mysqlはプリペアドステートメントのバイナリプロトコルで文字列を送るため、
// C言語のstrlen相当の「最初のNULで打ち切り」は起きない設計のはずだが、実際にDBへ
// 往復させて確認する(GORM/bob両ORMの実装が独立しているため、片方だけ壊れている
// 可能性もゼロではない)
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

func TestTask_NulByteAndControlChars_RoundTrip_GORM(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ(README参照)")
	}

	gormDB, err := db.New(dsn, slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}
	// 【テスト基盤監査で追加】internal/db.New自身のコメントが「呼び出し側でsql.DB経由の
	// Close()が必要」と明記しているにもかかわらず、この接続プールを閉じていなかった。
	// integrationパッケージ内のテストは同一プロセス内で逐次実行されるため、
	// Close()し忘れると1回のgo testプロセス中に開いた分だけコネクションプールが
	// 積み上がる(このパッケージ全体で他にも同様の未Close箇所が複数あるが、
	// このファイルのスコープ外のため個別修正はしない)
	if sqlDB, sqlErr := gormDB.DB(); sqlErr == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}

	userRepo := repository.NewUser(gormDB)
	taskRepo := repository.NewTask(gormDB)
	taskService := service.NewTaskService(taskRepo)
	ctx := context.Background()

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-nul-bytes", "nul-bytes@example.com", "NULバイト太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクの後始末に失敗: %v", err)
	}

	// nameは20文字以内制約があるため短く、descriptionには制限が無いのでNULを複数含める
	name := "a\x00b"
	description := "x\x00y\x01z\x1fw"
	finishedOn := time.Now().AddDate(0, 0, 7)

	created, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name: name, Description: &description, Status: "waiting", FinishedOn: finishedOn,
	})
	if err != nil {
		t.Fatalf("NULバイトを含むタスクの作成に失敗: %v", err)
	}

	got, err := taskService.Get(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("作成したタスクの取得に失敗: %v", err)
	}

	if diff := cmp.Diff(name, got.Name); diff != "" {
		t.Errorf("GORM経由でnameが往復で変化した(切り詰め等の疑い) (-want +got):\n%s", diff)
	}
	if got.Description == nil {
		t.Fatal("GORM経由でdescriptionがnilになった")
	}
	if diff := cmp.Diff(description, *got.Description); diff != "" {
		t.Errorf("GORM経由でdescriptionが往復で変化した(切り詰め等の疑い) (-want +got):\n%s", diff)
	}

	// 同じ行をbob経由(外部公開APIのlist実装)でも読み出し、GORM/bobで
	// 挙動が食い違っていないこと(ワイヤー契約パリティ)を確認する
	bobTasks, _, err := taskRepo.ListOffsetForExternalAPIBob(ctx, user.ID, 1, 20)
	if err != nil {
		t.Fatalf("bob経由でのリストに失敗: %v", err)
	}
	if len(bobTasks) != 1 {
		t.Fatalf("bob経由でのタスク件数 = %d, want 1", len(bobTasks))
	}
	if diff := cmp.Diff(name, bobTasks[0].Name); diff != "" {
		t.Errorf("bob経由でnameが往復で変化した(切り詰め等の疑い、GORMとの不整合) (-want +got):\n%s", diff)
	}
}
