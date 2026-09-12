package repository_test

// internal/repositoryには単体テストが無かったため新設する
// handler層のテスト(internal/handler/feature_flag_test.go)はリポジトリを介した
// 結合結果しか見ていない(HTMLレスポンスの文字列一致経由)ため、リポジトリの
// SQLロジック自体(List/Get/UpdateWithAuditLogのトランザクション/ListAuditLogs)を直接検証するテストとして追加する
// 既存のhandlerテストと同じ方針(実 MySQL の TEST_DB_DSN、testify 不使用、google/go-cmp)を踏襲する

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/repository"
)

const testRepoFlagKey = "test.admin-go-repository-test"

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}
	return db
}

// createTestFlag はテスト専用のflag_keyを冪等に(既存分を削除してから)作成する
func createTestFlag(t *testing.T, db *gorm.DB) *model.FeatureFlag {
	t.Helper()
	if err := db.Unscoped().Where("flag_key = ?", testRepoFlagKey).Delete(&model.FeatureFlag{}).Error; err != nil {
		t.Fatalf("既存テストフラグの削除に失敗: %v", err)
	}
	flag := model.FeatureFlag{
		FlagKey:          testRepoFlagKey,
		DefaultVariation: "off",
		Enabled:          true,
	}
	if err := db.Create(&flag).Error; err != nil {
		t.Fatalf("テストフラグ作成に失敗: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("feature_flag_id = ?", flag.ID).Delete(&model.FeatureFlagAuditLog{})
		db.Unscoped().Where("id = ?", flag.ID).Delete(&model.FeatureFlag{})
	})
	return &flag
}

func TestFeatureFlag_List_IncludesCreatedFlag(t *testing.T) {
	db := setupTestDB(t)
	flag := createTestFlag(t, db)
	repo := repository.NewFeatureFlag(db)

	flags, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List失敗: %v", err)
	}
	found := false
	for _, f := range flags {
		if f.ID == flag.ID {
			found = true
			if diff := cmp.Diff(testRepoFlagKey, f.FlagKey); diff != "" {
				t.Errorf("FlagKey mismatch (-want +got):\n%s", diff)
			}
		}
	}
	if !found {
		t.Errorf("List結果にflag_key=%q(id=%d)が含まれていない", testRepoFlagKey, flag.ID)
	}
}

func TestFeatureFlag_Get_ReturnsFlagByID(t *testing.T) {
	db := setupTestDB(t)
	flag := createTestFlag(t, db)
	repo := repository.NewFeatureFlag(db)

	got, err := repo.Get(context.Background(), flag.ID)
	if err != nil {
		t.Fatalf("Get失敗: %v", err)
	}
	if diff := cmp.Diff(testRepoFlagKey, got.FlagKey); diff != "" {
		t.Errorf("FlagKey mismatch (-want +got):\n%s", diff)
	}
}

func TestFeatureFlag_Get_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewFeatureFlag(db)

	if _, err := repo.Get(context.Background(), 999999999); err == nil {
		t.Error("存在しないIDでもエラーにならなかった")
	}
}

func TestFeatureFlag_UpdateWithAuditLog_PersistsBoth(t *testing.T) {
	db := setupTestDB(t)
	flag := createTestFlag(t, db)
	repo := repository.NewFeatureFlag(db)

	flag.DefaultVariation = "on"
	flag.Enabled = false
	before := "off"
	beforeEnabled := true
	auditLog := &model.FeatureFlagAuditLog{
		FeatureFlagID:          flag.ID,
		FlagKey:                flag.FlagKey,
		BeforeDefaultVariation: &before,
		AfterDefaultVariation:  "on",
		BeforeEnabled:          &beforeEnabled,
		AfterEnabled:           false,
		ChangedBy:              "repo-test",
		ChangedAt:              time.Now(),
	}

	if err := repo.UpdateWithAuditLog(context.Background(), flag, auditLog); err != nil {
		t.Fatalf("UpdateWithAuditLog失敗: %v", err)
	}

	got, err := repo.Get(context.Background(), flag.ID)
	if err != nil {
		t.Fatalf("Get失敗: %v", err)
	}
	if diff := cmp.Diff("on", got.DefaultVariation); diff != "" {
		t.Errorf("DefaultVariation mismatch (-want +got):\n%s", diff)
	}
	if got.Enabled {
		t.Errorf("Enabled = true, want false")
	}

	logs, err := repo.ListAuditLogs(context.Background(), flag.ID)
	if err != nil {
		t.Fatalf("ListAuditLogs失敗: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("監査ログ件数 = %d, want 1", len(logs))
	}
	if diff := cmp.Diff("repo-test", logs[0].ChangedBy); diff != "" {
		t.Errorf("ChangedBy mismatch (-want +got):\n%s", diff)
	}
}

func TestFeatureFlag_UpdateWithAuditLog_RollsBackOnAuditLogFailure(t *testing.T) {
	// 「監査ログ作成に失敗したらフラグ本体の更新もロールバックされる」というトランザクション境界そのものを検証する
	// flag_keyは空文字を入れても varchar(255) NOT NULLを満たしてしまう(空文字はNULLではない)ため、
	// 代わりにchanged_by(varchar(255))へ列長を超える文字列を与え、
	// strict modeの「Data too long for column」でCreateを確実に失敗させる
	db := setupTestDB(t)
	flag := createTestFlag(t, db)
	repo := repository.NewFeatureFlag(db)

	originalVariation := flag.DefaultVariation
	flag.DefaultVariation = "on"
	auditLog := &model.FeatureFlagAuditLog{
		FeatureFlagID:         flag.ID,
		FlagKey:               flag.FlagKey,
		AfterDefaultVariation: "on",
		AfterEnabled:          flag.Enabled,
		ChangedBy:             strings.Repeat("x", 300),
		ChangedAt:             time.Now(),
	}

	err := repo.UpdateWithAuditLog(context.Background(), flag, auditLog)
	if err == nil {
		t.Fatal("changed_byが列長を超えているのにCreateが失敗しなかった")
	}

	// ロールバックされ、フラグ本体は更新前の値のままであること
	got, getErr := repo.Get(context.Background(), flag.ID)
	if getErr != nil {
		t.Fatalf("Get失敗: %v", getErr)
	}
	if diff := cmp.Diff(originalVariation, got.DefaultVariation); diff != "" {
		t.Errorf("ロールバックされておらずDefaultVariationが更新されてしまっている (-want +got):\n%s", diff)
	}
}

func TestFeatureFlag_ListAuditLogs_OrdersNewestFirst(t *testing.T) {
	db := setupTestDB(t)
	flag := createTestFlag(t, db)
	repo := repository.NewFeatureFlag(db)

	// changed_atはdatetime(秒精度)であり、2回の time.Now() が同一秒内に収まると
	// タイブレークの順序がDB依存になり得るため、1件目と2件目のchanged_atを
	// 明示的に1秒以上離して確定的にする
	base := time.Now()
	for i, v := range []string{"on", "off"} {
		f, err := repo.Get(context.Background(), flag.ID)
		if err != nil {
			t.Fatalf("Get失敗(%d回目): %v", i, err)
		}
		f.DefaultVariation = v
		before := "off"
		if i > 0 {
			before = "on"
		}
		if err := repo.UpdateWithAuditLog(context.Background(), f, &model.FeatureFlagAuditLog{
			FeatureFlagID:          f.ID,
			FlagKey:                f.FlagKey,
			BeforeDefaultVariation: &before,
			AfterDefaultVariation:  v,
			AfterEnabled:           f.Enabled,
			ChangedBy:              "repo-test",
			ChangedAt:              base.Add(time.Duration(i) * 2 * time.Second),
		}); err != nil {
			t.Fatalf("UpdateWithAuditLog失敗(%d回目): %v", i, err)
		}
	}

	logs, err := repo.ListAuditLogs(context.Background(), flag.ID)
	if err != nil {
		t.Fatalf("ListAuditLogs失敗: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("監査ログ件数 = %d, want 2", len(logs))
	}
	if diff := cmp.Diff("off", logs[0].AfterDefaultVariation); diff != "" {
		t.Errorf("先頭(最新)のAfterDefaultVariation mismatch (-want +got):\n%s", diff)
	}
}
