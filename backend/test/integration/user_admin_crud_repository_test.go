//go:build integration

// repository.User.Create/Delete(CONTRACT.mdセクション17)を実MySQLで検証する
// service層(単体テスト)はfakeでガードロジックだけを見ており、実際のトランザクション・
// UNIQUE制約・FK制約順序(user_passwords/user_keycloaks/task_labels/tasks/webauthn_credentials→users)はここでしか確認できない
// 実行前提: 環境変数 TEST_DB_DSN(README参照)
package integration

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestUserRepository_CreateAndDelete(t *testing.T) {
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
	ctx := context.Background()

	const testEmail = "admin-crud-repo-test@example.com"
	// 再実行の度に重複INSERTでMySQL error 1062にならないよう、実行前に前回分を掃除する
	// (task_external_pagination_test.goと同じ「何度実行しても同じ結果になる」ための後始末)
	cleanup := func() {
		gormDB.Unscoped().Exec(
			"DELETE FROM user_passwords WHERE user_id IN (SELECT id FROM (SELECT id FROM users WHERE email = ?) AS u)", testEmail)
		gormDB.Unscoped().Exec(
			"DELETE FROM task_labels WHERE task_id IN (SELECT id FROM (SELECT id FROM tasks WHERE user_id IN (SELECT id FROM (SELECT id FROM users WHERE email = ?) AS u2)) AS t)", testEmail)
		gormDB.Unscoped().Exec(
			"DELETE FROM tasks WHERE user_id IN (SELECT id FROM (SELECT id FROM users WHERE email = ?) AS u)", testEmail)
		gormDB.Unscoped().Where("email = ?", testEmail).Delete(&model.User{})
	}
	cleanup()
	t.Cleanup(cleanup)

	expiresAt := time.Now().Add(24 * time.Hour)

	t.Run("Create: users/user_passwordsの両方に行ができる", func(t *testing.T) {
		user, err := userRepo.Create(ctx, "リポジトリ管理者テスト", testEmail, "dummy-digest", model.RoleGeneral, expiresAt)
		if err != nil {
			t.Fatalf("Create失敗: %v", err)
		}
		if user.ID == 0 {
			t.Fatalf("IDが採番されていない")
		}

		got, _, err := userRepo.GetByEmail(ctx, testEmail)
		if err != nil {
			t.Fatalf("GetByEmail失敗: %v", err)
		}
		if got.ID != user.ID || got.Name != "リポジトリ管理者テスト" {
			t.Errorf("Createした内容と取得結果が一致しない: got=%+v", got)
		}
	})

	t.Run("Create: email重複はエラーになる", func(t *testing.T) {
		_, err := userRepo.Create(ctx, "重複テスト", testEmail, "dummy-digest", model.RoleGeneral, expiresAt)
		if err == nil {
			t.Fatalf("email重複なのにエラーが返らなかった")
		}
	})

	t.Run("Delete: users/user_passwords/tasks/task_labelsが全部消え、他ユーザーには影響しない(FK順序が正しい)", func(t *testing.T) {
		user, _, err := userRepo.GetByEmail(ctx, testEmail)
		if err != nil {
			t.Fatalf("前提のGetByEmail失敗: %v", err)
		}

		// 【今回追加】以前は1タスク・ラベル無しでしか検証しておらず、
		// 「複数タスク×複数ラベル」かつ「他ユーザーへの影響が無いこと」が未検証だった
		labelRepo := repository.NewLabel(gormDB)
		label1 := model.Label{Name: "削除確認用ラベル1"}
		if err := labelRepo.Create(ctx, &label1); err != nil {
			t.Fatalf("テスト用label1作成失敗: %v", err)
		}
		label2 := model.Label{Name: "削除確認用ラベル2"}
		if err := labelRepo.Create(ctx, &label2); err != nil {
			t.Fatalf("テスト用label2作成失敗: %v", err)
		}
		t.Cleanup(func() { gormDB.Unscoped().Delete(&model.Label{}, label1.ID, label2.ID) })

		task1 := model.Task{Name: "削除対象タスク1", Status: model.TaskStatusWaiting, FinishedOn: time.Now(), UserID: user.ID,
			Labels: []model.Label{{ID: label1.ID}, {ID: label2.ID}}}
		if err := gormDB.Create(&task1).Error; err != nil {
			t.Fatalf("テスト用task1作成失敗: %v", err)
		}
		task2 := model.Task{Name: "削除対象タスク2", Status: model.TaskStatusWaiting, FinishedOn: time.Now(), UserID: user.ID,
			Labels: []model.Label{{ID: label1.ID}}}
		if err := gormDB.Create(&task2).Error; err != nil {
			t.Fatalf("テスト用task2作成失敗: %v", err)
		}

		// 【テスト監査で発見・追加】webauthn_credentials(migrations/000013)にはFK制約が無く、
		// 以前はrepository.User.Deleteの削除対象から漏れていた(削除後も
		// 存在しないuser_idを指す行が永久に残っていた実バグ、修正済み)。この回帰を確認する
		cred := model.WebauthnCredential{
			UserID:       user.ID,
			CredentialID: []byte("delete-cascade-test-credential-" + testEmail),
			PublicKey:    []byte("dummy-public-key"),
		}
		if err := gormDB.Create(&cred).Error; err != nil {
			t.Fatalf("テスト用webauthn_credential作成失敗: %v", err)
		}

		// 別ユーザーにも同じラベルを使うタスクを1件作り、Delete後もこちらは無傷であることを確認する
		const otherEmail = "admin-crud-repo-test-other@example.com"
		gormDB.Unscoped().Where("email = ?", otherEmail).Delete(&model.User{})
		otherUser, err := userRepo.Create(ctx, "巻き込まれ確認用", otherEmail, "dummy-digest", model.RoleGeneral, expiresAt)
		if err != nil {
			t.Fatalf("他ユーザー作成失敗: %v", err)
		}
		t.Cleanup(func() {
			gormDB.Unscoped().Exec("DELETE FROM task_labels WHERE task_id IN (SELECT id FROM (SELECT id FROM tasks WHERE user_id = ?) AS t)", otherUser.ID)
			gormDB.Unscoped().Where("user_id = ?", otherUser.ID).Delete(&model.Task{})
			gormDB.Unscoped().Where("user_id = ?", otherUser.ID).Delete(&model.UserPassword{})
			gormDB.Unscoped().Delete(&model.User{}, otherUser.ID)
		})
		otherTask := model.Task{Name: "他ユーザーのタスク", Status: model.TaskStatusWaiting, FinishedOn: time.Now(), UserID: otherUser.ID,
			Labels: []model.Label{{ID: label1.ID}}}
		if err := gormDB.Create(&otherTask).Error; err != nil {
			t.Fatalf("他ユーザーのtask作成失敗: %v", err)
		}

		if err := userRepo.Delete(ctx, user.ID); err != nil {
			t.Fatalf("Delete失敗: %v", err)
		}

		if _, _, err := userRepo.GetByEmail(ctx, testEmail); err == nil {
			t.Errorf("Delete後もusersが残っている")
		}
		var pwCount int64
		gormDB.Model(&model.UserPassword{}).Where("user_id = ?", user.ID).Count(&pwCount)
		if pwCount != 0 {
			t.Errorf("Delete後もuser_passwordsが残っている: count=%d", pwCount)
		}
		var taskCount int64
		gormDB.Model(&model.Task{}).Where("user_id = ?", user.ID).Count(&taskCount)
		if taskCount != 0 {
			t.Errorf("Delete後もtasksが残っている: count=%d", taskCount)
		}
		var taskLabelCount int64
		gormDB.Model(&model.TaskLabel{}).Where("task_id IN (?)", []uint64{task1.ID, task2.ID}).Count(&taskLabelCount)
		if taskLabelCount != 0 {
			t.Errorf("Delete後もtask_labelsが残っている: count=%d", taskLabelCount)
		}
		var credCount int64
		gormDB.Model(&model.WebauthnCredential{}).Where("id = ?", cred.ID).Count(&credCount)
		if credCount != 0 {
			t.Errorf("Delete後もwebauthn_credentialsが残っている(存在しないuser_idを指す孤立行): count=%d", credCount)
		}

		// 他ユーザー・共有していたラベル自体は無傷であること
		var otherTaskCount int64
		gormDB.Model(&model.Task{}).Where("user_id = ?", otherUser.ID).Count(&otherTaskCount)
		if otherTaskCount != 1 {
			t.Errorf("他ユーザーのtaskが巻き込まれて消えた: count=%d, want 1", otherTaskCount)
		}
		var labelCount int64
		gormDB.Model(&model.Label{}).Where("id IN (?)", []uint64{label1.ID, label2.ID}).Count(&labelCount)
		if labelCount != 2 {
			t.Errorf("labels自体が巻き込まれて消えた: count=%d, want 2", labelCount)
		}
	})

	t.Run("Delete: 存在しないIDはgorm.ErrRecordNotFound相当のエラーになる", func(t *testing.T) {
		if err := userRepo.Delete(ctx, 999999999); err == nil {
			t.Errorf("存在しないIDのDeleteがエラーにならなかった")
		}
	})
}

// TestUserService_LastManagerGuard_RealDB は「最後の管理者は降格/削除できない」ガードを実 DB(repository.User.CountManagement/Get)を通して検証する
// 既存のservice層単体テストは fakeUserRepo でガードロジックだけを見ており、
// CountManagementの実SQL(WHERE role = ?)の挙動込みでは未検証だったため追加する
//
// 【テスト作成中に自己発見・修正した問題】
// このプロジェクトの開発用DBは長期間の手動検証で
// 状態が積み重なっており、実際にseedユーザー(local-user@example.com)がrole=managementに
// 変わっていたり、Keycloak側のadmin-userが既にmanagementとして存在したりする
//
// CountManagementはテーブル全体を数える実装のため、「テスト用に2人作って1人消す」だけの
// 素朴な検証では、既存の管理者の存在によってガードが発火せずテストが不安定になる
// (実際に一度この形で書いて、既存データの状態次第でfail/passが変わることを確認した)
//
// そのため、DBトランザクション内で既存の管理者を一時的にgeneralへ退避させ、
// テスト終了時に必ずロールバック(コミットしない)することで、共有DBの実データには
// 一切影響を与えずに「管理者が自分の作った2人だけ」という状態を再現する
func TestUserService_LastManagerGuard_RealDB(t *testing.T) {
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

	tx := gormDB.Begin()
	defer tx.Rollback()

	// 既存の管理者を全員このトランザクション内だけでgeneralへ退避させる(コミットしないので他プロセス・実データには一切見えない・影響しない)
	if err := tx.Model(&model.User{}).Where("role = ?", model.RoleManagement).
		Update("role", model.RoleGeneral).Error; err != nil {
		t.Fatalf("既存管理者の退避に失敗: %v", err)
	}

	userRepo := repository.NewUser(tx)
	userService := service.NewUserService(userRepo)
	ctx := context.Background()

	expiresAt := time.Now().Add(24 * time.Hour)
	admin1, err := userRepo.Create(ctx, "実DB管理者1", "admin-crud-guard-test-1@example.com", "dummy-digest", model.RoleManagement, expiresAt)
	if err != nil {
		t.Fatalf("admin1作成失敗: %v", err)
	}
	admin2, err := userRepo.Create(ctx, "実DB管理者2", "admin-crud-guard-test-2@example.com", "dummy-digest", model.RoleManagement, expiresAt)
	if err != nil {
		t.Fatalf("admin2作成失敗: %v", err)
	}

	// 管理者が2人いる状態: 片方の削除は成功するはず
	if err := userService.Delete(ctx, admin1.ID); err != nil {
		t.Fatalf("管理者2人中1人の削除が失敗した: %v", err)
	}

	// 残り1人になった状態: この管理者の削除は拒否されるはず
	err = userService.Delete(ctx, admin2.ID)
	if !errors.Is(err, service.ErrLastManagerUser) {
		t.Fatalf("最後の管理者を削除しようとした結果 = %v, want ErrLastManagerUser", err)
	}

	// UpdateRoleでの降格も同様に拒否されるはず
	err = userService.UpdateRole(ctx, admin2.ID, model.RoleGeneral)
	if !errors.Is(err, service.ErrLastManagerUser) {
		t.Fatalf("最後の管理者を降格しようとした結果 = %v, want ErrLastManagerUser", err)
	}
}

// TestUserService_Create_ConcurrentSameEmail は、同じemailで
// POST /internal/v1/admin/users相当の作成をほぼ同時に実行した場合、
// 片方だけが成功しもう片方は500ではなく ErrEmailTaken(422相当)として
// 処理されることを検証する(CONTRACT.mdセクション17.2)
//
// repository.User.Createはusers.emailのUNIQUE制約(migrations/000001)に守られており、
// MySQL側が直列化してくれるため、アプリケーション側で追加の排他制御は不要なはずだが、
// 実際にgoroutineで同時実行して確認するまではあくまで「設計上そうなるはず」でしかないため実証する
func TestUserService_Create_ConcurrentSameEmail(t *testing.T) {
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
	userService := service.NewUserService(userRepo)
	ctx := context.Background()

	const concurrentEmail = "admin-crud-concurrent-test@example.com"
	// 再実行時に前回分が残っていないよう掃除しておく(他のテストと同じ後始末パターン)
	cleanupConcurrent := func() {
		var u model.User
		if err := gormDB.Where("email = ?", concurrentEmail).First(&u).Error; err == nil {
			gormDB.Where("user_id = ?", u.ID).Delete(&model.UserPassword{})
			gormDB.Delete(&model.User{}, u.ID)
		}
	}
	cleanupConcurrent()
	defer cleanupConcurrent()

	const n = 5
	results := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := userService.Create(ctx, "同時作成テスト", concurrentEmail, "password", model.RoleGeneral)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	successCount := 0
	emailTakenCount := 0
	for err := range results {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, service.ErrEmailTaken):
			emailTakenCount++
		default:
			t.Errorf("想定外のエラー(500になっていないか要確認): %v", err)
		}
	}

	if successCount != 1 {
		t.Errorf("成功件数 = %d, want 1(ちょうど1件だけ作成に成功するはず)", successCount)
	}
	if emailTakenCount != n-1 {
		t.Errorf("ErrEmailTaken件数 = %d, want %d(残り全件がemail重複として処理されるはず)", emailTakenCount, n-1)
	}
}
