//go:build integration

// 2回目のテスト監査で発覚した実バグ(TOCTOUレース)の再現・回帰防止テスト:
//
// 以前は「最後の管理者は降格・削除できない」ガードが、
// 参照(SELECT)と更新(UPDATE/DELETE)を別々の非トランザクションなクエリとして実行していたため、
// managementがちょうど2人のときに2つの削除/降格リクエストがほぼ同時に届くと、
// 両方とも「今はまだ2人いる」という参照結果を見た直後にそれぞれ更新を実行してしまい、
// 両方成功してmanagementが0人になり得た
// (=誰もFeature Flag/ユーザー管理画面を操作できなくなり、DBを直接触るしか復旧手段が無くなる、という実際に起こり得た重大な不整合)
//
// repository.User.checkNotLastManagerLocked(SELECT ... FOR UPDATE)による修正後は、
// 2つの並行トランザクションのうち片方が必ずもう片方のCOMMIT/ROLLBACKを待たされるため、
// 常にどちらか一方だけが成功することを、実際にgoroutineで並行実行して確認する
package integration

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestUserService_LastManagerGuard_ConcurrentDeleteRace(t *testing.T) {
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

	emails := []string{"race-admin-1@example.com", "race-admin-2@example.com"}
	// 再実行しても同じ結果になるように、前回実行分の後始末を先に行う
	cleanupRaceAdmins(t, gormDB, emails)
	t.Cleanup(func() { cleanupRaceAdmins(t, gormDB, emails) })

	// 【重要】
	// このガードは「システム全体でmanagementが何人いるか」というグローバルな状態を見るため、
	// この2人だけで検証するには、この2人以外の既存のmanagement
	// (実機検証で作ったgeneral-user@example.com等)
	// を一時的にコミット付きで退避させる必要がある
	// (このテストの2接続がそれぞれ別のMySQLコネクションを使う以上、
	//「見えている行の集合」を揃えるには実際にコミットするしかなく、
	// ロールバックされるトランザクション内に隠しても他方の接続からは見えない)
	//
	// テスト内で確実に元の状態へ戻すことを保証するため、必ずt.Cleanupで復元する(テストが途中でpanicしても実行される)
	var others []model.User
	if err := gormDB.Where("role = ?", model.RoleManagement).Find(&others).Error; err != nil {
		t.Fatalf("既存管理者一覧の取得に失敗: %v", err)
	}
	if len(others) > 0 {
		var otherIDs []uint64
		for _, u := range others {
			otherIDs = append(otherIDs, u.ID)
		}
		if err := gormDB.Model(&model.User{}).Where("id IN ?", otherIDs).
			Update("role", model.RoleGeneral).Error; err != nil {
			t.Fatalf("既存管理者の一時退避に失敗: %v", err)
		}
		t.Cleanup(func() {
			if err := gormDB.Model(&model.User{}).Where("id IN ?", otherIDs).
				Update("role", model.RoleManagement).Error; err != nil {
				t.Errorf("既存管理者の復元に失敗した(手動での復旧が必要): %v", err)
			}
		})
	}

	userRepo := repository.NewUser(gormDB)
	userService := service.NewUserService(userRepo)
	ctx := context.Background()

	expiresAt := time.Now().Add(24 * time.Hour)
	admin1, err := userRepo.Create(ctx, "競合太郎1", emails[0], "dummy-digest", model.RoleManagement, expiresAt)
	if err != nil {
		t.Fatalf("admin1作成失敗: %v", err)
	}
	admin2, err := userRepo.Create(ctx, "競合太郎2", emails[1], "dummy-digest", model.RoleManagement, expiresAt)
	if err != nil {
		t.Fatalf("admin2作成失敗: %v", err)
	}

	before, err := userRepo.CountManagement(ctx)
	if err != nil {
		t.Fatalf("事前のmanagement人数取得に失敗: %v", err)
	}
	if before != 2 {
		t.Fatalf("前提条件が崩れている: management人数 = %d, want 2(他の管理者の退避に失敗している可能性)", before)
	}

	// 【本題】managementがちょうど2人(admin1・admin2)の状態で、2つのゴルーチンが
	// 「ほぼ同時に」それぞれ別の管理者を削除しようとする
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		results <- userService.Delete(ctx, admin1.ID)
	}()
	go func() {
		defer wg.Done()
		<-start
		results <- userService.Delete(ctx, admin2.ID)
	}()
	close(start) // 2つのゴルーチンをほぼ同時に走らせ、レースウィンドウを最大化する
	wg.Wait()
	close(results)

	var succeeded, rejected int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, service.ErrLastManagerUser):
			rejected++
		default:
			t.Fatalf("想定外のエラー: %v", err)
		}
	}

	// 【回帰の本体】修正前はここが succeeded=2, rejected=0 になり得た(両方成功=バグ再現、
	// 実際に修正前のコードに戻して確認済み)
	// 修正後は必ずどちらか一方だけが成功する
	if succeeded != 1 || rejected != 1 {
		t.Errorf("succeeded=%d rejected=%d, want 1 and 1(TOCTOUレースにより両方成功した=修正前の挙動に戻っている)", succeeded, rejected)
	}

	after, err := userRepo.CountManagement(ctx)
	if err != nil {
		t.Fatalf("事後のmanagement人数取得に失敗: %v", err)
	}
	if before-after != 1 {
		t.Errorf("management人数の減少 = %d, want 1(2人同時に減っていたら重大な不整合)", before-after)
	}
}

func cleanupRaceAdmins(t *testing.T, gormDB *gorm.DB, emails []string) {
	t.Helper()
	var userIDs []uint64
	gormDB.Model(&model.User{}).Where("email IN ?", emails).Pluck("id", &userIDs)
	if len(userIDs) == 0 {
		return
	}
	if err := gormDB.Where("user_id IN ?", userIDs).Delete(&model.UserPassword{}).Error; err != nil {
		t.Fatalf("user_passwordsの後始末に失敗: %v", err)
	}
	if err := gormDB.Where("id IN ?", userIDs).Delete(&model.User{}).Error; err != nil {
		t.Fatalf("usersの後始末に失敗: %v", err)
	}
}
