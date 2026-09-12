//go:build integration

// 【4回目のテスト監査(ワイヤー契約パリティ・並行性の新角度)で発覚・修正した実バグ】
//
// JITプロビジョニング(UserService.Provision → repository.User.UpsertByKeycloakSub)は、
// メソッド名に反して原子的なUPSERTではなく「SELECTで見つからなければCREATE」という
// 素朴な2段構えの実装だった。同じKeycloak subに対して2つのリクエストがほぼ
// 同時に届くと(例: 同じユーザーが2つのタブで同時にログインを完了させる。今セッションで
// frontend-rails/without-bffもログインのたびにこのProvisionを呼ぶようになったため、
// この経路を通るクライアントが増えている)、両方ともSELECTで「まだ居ない」を見た直後に
// それぞれCREATEを実行してしまい、2件目以降のCREATEがusers.email/
// user_keycloaks.keycloak_subのUNIQUE制約違反で失敗していた。このエラーは
// isDuplicateEmailErrorのような判定を経ずにそのままハンドラまで伝播し、クライアントには
// 生の500相当のエラーが返っていた(実際に10並行リクエストで再現し、修正前は
// 平均8割前後が失敗することを確認した)。
//
// (「最後の管理者」ガードのTOCTOUレース(user_last_manager_race_test.go)と同じクラスの
// 並行性バグだが、Provisionには同種の対策が入っていなかった)
//
// 修正: repository.isDuplicateUserRaceError で重複エラーを検出した場合、失敗として
// 返さず改めてSELECTし直し、並行して作成された既存行を返すようにした
// (Rails版find_or_initialize_byの実質的な原子性に合わせる)
//
// このテストは、10並行リクエスト全てが成功し、かつ全員が同じuser_idに解決される
// (=行が重複作成されていない)ことを固定する
package integration

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestUserService_Provision_ConcurrentSameNewKeycloakSubRace(t *testing.T) {
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

	const keycloakSub = "jit-race-test-sub-12345"
	const email = "jit-race-test@example.com"
	cleanupJITRaceUser(t, gormDB, keycloakSub, email)
	t.Cleanup(func() { cleanupJITRaceUser(t, gormDB, keycloakSub, email) })

	userRepo := repository.NewUser(gormDB)
	svc := service.NewUserService(userRepo)

	const n = 10
	errs := make([]error, n)
	ids := make([]uint64, n)
	var wg sync.WaitGroup
	// 【重要】全goroutineの起動が揃うようにbarrierで足並みを揃える(sync.WaitGroupだけだと
	// 起動順にばらつきが出て、先に開始したgoroutineが先にINSERTまで終えてしまい、
	// 「両方ともSELECTで見つからないと判定した直後にCREATEする」という本来のレース条件を
	// 再現できない可能性があるため)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			dto, err := svc.Provision(context.Background(), keycloakSub, "JITレースくん", email, nil)
			errs[idx] = err
			ids[idx] = dto.ID
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine[%d]が失敗した(修正後は10並行リクエスト全てが成功するはず): %v", i, err)
		}
	}

	// 全goroutineが同じuser_idに解決されること(=usersテーブルに重複行が
	// 作られていないこと)を確認する。isDuplicateUserRaceErrorでの復旧が
	// 正しく「既存行を読み直す」動作になっていることの裏付け
	firstID := ids[0]
	for i, id := range ids {
		if id != firstID {
			t.Errorf("goroutine[%d]のuser_id=%dが他と異なる(想定はfirstID=%d): 行が重複作成された疑い", i, id, firstID)
		}
	}

	var count int64
	if err := gormDB.Table("users").Where("email = ?", email).Count(&count).Error; err != nil {
		t.Fatalf("usersの件数取得に失敗: %v", err)
	}
	if count != 1 {
		t.Errorf("email=%sのusers行数 = %d, want 1(重複作成されていないこと)", email, count)
	}
}

func cleanupJITRaceUser(t *testing.T, gormDB *gorm.DB, keycloakSub, email string) {
	t.Helper()
	var userIDs []uint64
	gormDB.
		Table("users").
		Joins("LEFT JOIN user_keycloaks ON user_keycloaks.user_id = users.id").
		Where("users.email = ? OR user_keycloaks.keycloak_sub = ?", email, keycloakSub).
		Pluck("users.id", &userIDs)
	if len(userIDs) == 0 {
		return
	}
	if err := gormDB.Where("user_id IN ?", userIDs).Delete(&model.UserKeycloak{}).Error; err != nil {
		t.Fatalf("user_keycloaksの後始末に失敗: %v", err)
	}
	if err := gormDB.Where("id IN ?", userIDs).Delete(&model.User{}).Error; err != nil {
		t.Fatalf("usersの後始末に失敗: %v", err)
	}
}
