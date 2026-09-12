//go:build integration

// CONTRACT.mdセクション24: GORM実装(repository.Task.ListOffsetForExternalAPI /
// ListCursorForExternalAPI)とbob実装(...Bob サフィックス版)が、同じデータに対して
// 完全に同じ結果を返すことを確認する結合テスト
//
// test/integration/task_v1_v2_compare_test.go(v1のN+1実装とv2のPreload実装の突き合わせ)と
// 同じ考え方を、「ORMの実装違い」という別の軸に対して適用したもの
// go-cmpで model.Task のスライスをそのまま比較する(DTOへの変換より手前、repository層の
// 戻り値そのものを比較することで、GORMのPreload("Labels")とbobのattachLabelsBobが
// 本当に同じ集合を組み立てているかまで確認できる)
//
// 実行前提: 環境変数 TEST_DB_DSN に疎通可能なMySQL DSNを設定しておくこと(README参照)
package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// sortLabelsByID はGORMのPreload("Labels")・bob版attachLabelsBobのどちらも、
// task_labelsをどの順で返すかはMySQL任せ(明示的なORDER BYが無い)であるため、
// Labelsスライスの要素順そのものは「同じ挙動」の対象に含めず、IDでソートしてから比較する
var sortLabelsByID = cmpopts.SortSlices(func(a, b model.Label) bool { return a.ID < b.ID })

func TestListExternalAPI_GormAndBob_ReturnSameTasks(t *testing.T) {
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

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-external-orm-compare", "external-orm-compare@example.com", "ORM比較太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}

	// task_v1_v2_compare_test.goと同じ理由: 再実行しても結果が変わらないよう、
	// 実行前にこのユーザーの既存タスク・テスト用ラベルを後始末しておく
	var prevTaskIDs []uint64
	gormDB.Model(&model.Task{}).Where("user_id = ?", user.ID).Pluck("id", &prevTaskIDs)
	if len(prevTaskIDs) > 0 {
		if err := gormDB.Exec("DELETE FROM task_labels WHERE task_id IN ?", prevTaskIDs).Error; err != nil {
			t.Fatalf("既存task_labelsの後始末に失敗: %v", err)
		}
	}
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクの後始末に失敗: %v", err)
	}
	for _, name := range []string{"ORM比較用A", "ORM比較用B"} {
		if err := gormDB.Unscoped().Where("name = ?", name).Delete(&model.Label{}).Error; err != nil {
			t.Fatalf("既存ラベルの後始末に失敗(%s): %v", name, err)
		}
	}

	labelA, err := labelService.Create(ctx, "ORM比較用A")
	if err != nil {
		t.Fatalf("ラベルA作成失敗: %v", err)
	}
	labelB, err := labelService.Create(ctx, "ORM比較用B")
	if err != nil {
		t.Fatalf("ラベルB作成失敗: %v", err)
	}

	// ラベル0/1/2個のタスクを混在させ、attachLabelsBob(labelなし・1件・複数件)の
	// 全パターンをカバーする
	finishedOn := time.Now().AddDate(0, 0, 7)
	const total = 15
	for i := 0; i < total; i++ {
		var labelIDs []uint64
		switch i % 3 {
		case 0:
			labelIDs = nil
		case 1:
			labelIDs = []uint64{labelA.ID}
		case 2:
			labelIDs = []uint64{labelA.ID, labelB.ID}
		}
		if _, err := taskService.Create(ctx, user.ID, service.TaskInput{
			Name:       "ORM比較タスク",
			Status:     "waiting",
			FinishedOn: finishedOn,
			LabelIDs:   labelIDs,
		}); err != nil {
			t.Fatalf("タスク作成失敗(i=%d): %v", i, err)
		}
	}

	// GORM版との突き合わせだけでなく、bob実装単体の振る舞いも直接検証しておく
	// (「GORM版もbob版も同じ間違え方をしている」ケースを突き合わせテストだけでは検出できないため)
	t.Run("bob単体: ページング境界とラベル同時取得の直接検証", func(t *testing.T) {
		// page 1(4件)〜4ページ目(3件)〜5ページ目(0件)まで境界を確認する
		firstPage, firstTotal, err := taskRepo.ListOffsetForExternalAPIBob(ctx, user.ID, 1, 4)
		if err != nil {
			t.Fatalf("1ページ目取得失敗: %v", err)
		}
		if int(firstTotal) != total {
			t.Errorf("total = %d, want %d", firstTotal, total)
		}
		if len(firstPage) != 4 {
			t.Errorf("1ページ目件数 = %d, want 4", len(firstPage))
		}

		lastPage, _, err := taskRepo.ListOffsetForExternalAPIBob(ctx, user.ID, 4, 4)
		if err != nil {
			t.Fatalf("4ページ目取得失敗: %v", err)
		}
		if len(lastPage) != 3 { // 15件 / 4件ずつ = 3ページ+3件
			t.Errorf("4ページ目件数 = %d, want 3", len(lastPage))
		}

		emptyPage, _, err := taskRepo.ListOffsetForExternalAPIBob(ctx, user.ID, 5, 4)
		if err != nil {
			t.Fatalf("5ページ目取得失敗: %v", err)
		}
		if len(emptyPage) != 0 {
			t.Errorf("5ページ目件数 = %d, want 0(範囲外)", len(emptyPage))
		}

		// ラベル0件/1件/2件のタスクがそれぞれ正しいLabels(名前まで)を持つことを確認する
		// (i%3の割り当て順はcreated_atの新しい順=ソート順とは逆になる点に注意し、名前の集合で見る)
		gotLabelCounts := map[int]int{} // ラベル件数 -> 出現回数
		for _, task := range firstPage {
			gotLabelCounts[len(task.Labels)]++
			for _, l := range task.Labels {
				if l.Name != "ORM比較用A" && l.Name != "ORM比較用B" {
					t.Errorf("task id=%d に想定外のラベルが付いている: %+v", task.ID, l)
				}
				if l.ID == 0 || l.CreatedAt.IsZero() {
					t.Errorf("task id=%d のラベルが不完全にしか埋まっていない: %+v", task.ID, l)
				}
			}
		}
		if gotLabelCounts[0]+gotLabelCounts[1]+gotLabelCounts[2] != len(firstPage) {
			t.Errorf("ラベル件数の内訳が不整合: %v (len=%d)", gotLabelCounts, len(firstPage))
		}

		// cursorページングも同様に終端を確認する
		allViaCursor := make([]model.Task, 0, total)
		var cursor *repository.TaskCursor
		for i := 0; i < total/4+3; i++ {
			page, err := taskRepo.ListCursorForExternalAPIBob(ctx, user.ID, cursor, 4)
			if err != nil {
				t.Fatalf("cursorページ取得失敗(i=%d): %v", i, err)
			}
			if len(page) == 0 {
				break
			}
			allViaCursor = append(allViaCursor, page...)
			last := page[len(page)-1]
			cursor = &repository.TaskCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		}
		if len(allViaCursor) != total {
			t.Errorf("cursorページングで集めたタスク数 = %d, want %d", len(allViaCursor), total)
		}
	})

	t.Run("offset: 全ページを通して完全に同じ結果", func(t *testing.T) {
		const pageSize = 4
		for page := 1; page <= total/pageSize+2; page++ {
			gormTasks, gormTotal, err := taskRepo.ListOffsetForExternalAPI(ctx, user.ID, page, pageSize)
			if err != nil {
				t.Fatalf("ListOffsetForExternalAPI失敗(page=%d): %v", page, err)
			}
			bobTasks, bobTotal, err := taskRepo.ListOffsetForExternalAPIBob(ctx, user.ID, page, pageSize)
			if err != nil {
				t.Fatalf("ListOffsetForExternalAPIBob失敗(page=%d): %v", page, err)
			}

			if gormTotal != bobTotal {
				t.Errorf("page=%d: total不一致 gorm=%d bob=%d", page, gormTotal, bobTotal)
			}
			if diff := cmp.Diff(gormTasks, bobTasks, sortLabelsByID); diff != "" {
				t.Errorf("page=%d: GORMとbobで結果が一致しない (-gorm +bob):\n%s", page, diff)
			}
		}
	})

	t.Run("cursor: 全ページを通して完全に同じ結果", func(t *testing.T) {
		const limit = 4
		var gormCursor, bobCursor *repository.TaskCursor
		page := 0
		for {
			page++
			if page > total/limit+3 {
				t.Fatal("cursorページングが終端しない(無限ループの疑い)")
			}

			gormTasks, err := taskRepo.ListCursorForExternalAPI(ctx, user.ID, gormCursor, limit)
			if err != nil {
				t.Fatalf("ListCursorForExternalAPI失敗(page=%d): %v", page, err)
			}
			bobTasks, err := taskRepo.ListCursorForExternalAPIBob(ctx, user.ID, bobCursor, limit)
			if err != nil {
				t.Fatalf("ListCursorForExternalAPIBob失敗(page=%d): %v", page, err)
			}

			if diff := cmp.Diff(gormTasks, bobTasks, sortLabelsByID); diff != "" {
				t.Fatalf("page=%d: GORMとbobで結果が一致しない (-gorm +bob):\n%s", page, diff)
			}

			if len(gormTasks) == 0 {
				break
			}
			last := gormTasks[len(gormTasks)-1]
			gormCursor = &repository.TaskCursor{CreatedAt: last.CreatedAt, ID: last.ID}
			bobCursor = gormCursor // 直前の突き合わせでgorm/bobの結果が同一と確認済みなので使い回せる

			if len(gormTasks) < limit {
				break
			}
		}
	})

	// service層(ListExternalV1/ListExternalV1Bob、ListExternalV2/ListExternalV2Bob)まで
	// 通しても同じDTOになることも確認しておく(repository層の一致だけでは、
	// service層のtoTaskDTO変換にORM別の分岐バグが紛れ込んでいないかまでは保証できないため)
	t.Run("service層: ListExternalV1/V1Bob・ListExternalV2/V2Bobも一致", func(t *testing.T) {
		gormDTOs, gormTotal, err := taskService.ListExternalV1(ctx, service.ExternalListFilterV1{UserID: user.ID, Page: 1, PageSize: total})
		if err != nil {
			t.Fatalf("ListExternalV1失敗: %v", err)
		}
		bobDTOs, bobTotal, err := taskService.ListExternalV1Bob(ctx, service.ExternalListFilterV1{UserID: user.ID, Page: 1, PageSize: total})
		if err != nil {
			t.Fatalf("ListExternalV1Bob失敗: %v", err)
		}
		if gormTotal != bobTotal {
			t.Errorf("total不一致 gorm=%d bob=%d", gormTotal, bobTotal)
		}
		if diff := cmp.Diff(gormDTOs, bobDTOs, cmpopts.SortSlices(func(a, b service.LabelDTO) bool { return a.ID < b.ID })); diff != "" {
			t.Errorf("ListExternalV1とListExternalV1Bobで結果が一致しない (-gorm +bob):\n%s", diff)
		}

		gormV2DTOs, gormNext, err := taskService.ListExternalV2(ctx, service.ExternalListFilterV2{UserID: user.ID, Limit: total})
		if err != nil {
			t.Fatalf("ListExternalV2失敗: %v", err)
		}
		bobV2DTOs, bobNext, err := taskService.ListExternalV2Bob(ctx, service.ExternalListFilterV2{UserID: user.ID, Limit: total})
		if err != nil {
			t.Fatalf("ListExternalV2Bob失敗: %v", err)
		}
		if gormNext != bobNext {
			t.Errorf("next_cursor不一致 gorm=%q bob=%q", gormNext, bobNext)
		}
		if diff := cmp.Diff(gormV2DTOs, bobV2DTOs, cmpopts.SortSlices(func(a, b service.LabelDTO) bool { return a.ID < b.ID })); diff != "" {
			t.Errorf("ListExternalV2とListExternalV2Bobで結果が一致しない (-gorm +bob):\n%s", diff)
		}
	})
}
