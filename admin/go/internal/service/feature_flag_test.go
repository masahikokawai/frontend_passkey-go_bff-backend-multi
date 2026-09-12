package service

import (
	"context"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/repository"
)

// このテストは実MySQL(TEST_DB_DSN、feature_flagsテーブル作成済み)を前提にする
// backend/migrationsのgolang-migrateでテーブル作成済みの環境でのみ実行される
// (未設定/未作成ならスキップする。既存backend/bffの結合テスト方針を踏襲)
func TestFeatureFlagService_Update_RecordsAuditLog(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ")
	}

	gormDB, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}

	const testFlagKey = "test.admin-go-service-update"
	ctx := context.Background()

	// テスト用フラグを毎回クリーンな状態から作る(冪等性のため既存分を削除してから作成)
	if err := gormDB.Unscoped().Where("flag_key = ?", testFlagKey).Delete(&model.FeatureFlag{}).Error; err != nil {
		t.Fatalf("既存テストフラグの削除に失敗: %v", err)
	}
	flag := model.FeatureFlag{
		FlagKey:          testFlagKey,
		DefaultVariation: "off",
		Enabled:          true,
	}
	if err := gormDB.Create(&flag).Error; err != nil {
		t.Fatalf("テストフラグ作成に失敗: %v", err)
	}
	t.Cleanup(func() {
		gormDB.Unscoped().Where("feature_flag_id = ?", flag.ID).Delete(&model.FeatureFlagAuditLog{})
		gormDB.Unscoped().Where("id = ?", flag.ID).Delete(&model.FeatureFlag{})
	})

	repo := repository.NewFeatureFlag(gormDB)
	svc := NewFeatureFlagService(repo)

	if err := svc.Update(ctx, flag.ID, UpdateInput{
		Enabled:          true,
		DefaultVariation: "on",
		ChangedBy:        "test-admin",
	}); err != nil {
		t.Fatalf("Update失敗: %v", err)
	}

	got, err := svc.Get(ctx, flag.ID)
	if err != nil {
		t.Fatalf("Get失敗: %v", err)
	}
	if got.DefaultVariation != "on" {
		t.Errorf("DefaultVariation = %q, want %q", got.DefaultVariation, "on")
	}

	logs, err := svc.AuditLogs(ctx, flag.ID)
	if err != nil {
		t.Fatalf("AuditLogs失敗: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("監査ログ件数 = %d, want 1", len(logs))
	}
	if diff := cmp.Diff("off", *logs[0].BeforeDefaultVariation); diff != "" {
		t.Errorf("BeforeDefaultVariation mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("on", logs[0].AfterDefaultVariation); diff != "" {
		t.Errorf("AfterDefaultVariation mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("test-admin", logs[0].ChangedBy); diff != "" {
		t.Errorf("ChangedBy mismatch (-want +got):\n%s", diff)
	}
}

// 【CONTRACT.mdセクション19で変更】以前はDefaultVariationが"on"/"off"かどうかを
// repoに触れる前に検証していたため、このテストはrepo:nilでもDBレスで実行できていた
//
// 多値(multivariate)フラグ対応により、検証対象の値はそのフラグ自身のVariationsに
// 依存するようになったため、実際にフラグをDBから取得する必要がある
// (=TEST_DB_DSN前提のテストへ変わった上のTestFeatureFlagService_Update_RecordsAuditLogと同じ冪等化パターンを踏襲する)
func TestFeatureFlagService_Update_RejectsInvalidVariation(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ")
	}

	gormDB, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}

	const testFlagKey = "test.admin-go-service-reject-invalid"
	ctx := context.Background()

	if err := gormDB.Unscoped().Where("flag_key = ?", testFlagKey).Delete(&model.FeatureFlag{}).Error; err != nil {
		t.Fatalf("既存テストフラグの削除に失敗: %v", err)
	}
	flag := model.FeatureFlag{
		FlagKey:          testFlagKey,
		DefaultVariation: "off",
		Variations:       strPtr(`{"on":true,"off":false}`),
		Enabled:          true,
	}
	if err := gormDB.Create(&flag).Error; err != nil {
		t.Fatalf("テストフラグ作成に失敗: %v", err)
	}
	t.Cleanup(func() {
		gormDB.Unscoped().Where("id = ?", flag.ID).Delete(&model.FeatureFlag{})
	})

	repo := repository.NewFeatureFlag(gormDB)
	svc := NewFeatureFlagService(repo)

	updateErr := svc.Update(ctx, flag.ID, UpdateInput{
		Enabled:          true,
		DefaultVariation: "maybe", // variationsに存在しない値
		ChangedBy:        "test-admin",
	})
	if updateErr == nil {
		t.Fatal("不正なDefaultVariationでエラーにならなかった")
	}
}

// TestFeatureFlagService_Update_AcceptsMultivariateFlag はCONTRACT.mdセクション19の
// 多値フラグ(frontend.task-create-uxを模した3値)で、on/off以外の値が正しく
// 受理されることを確認する回帰テスト
func TestFeatureFlagService_Update_AcceptsMultivariateFlag(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ")
	}

	gormDB, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}

	const testFlagKey = "test.admin-go-service-multivariate"
	ctx := context.Background()

	if err := gormDB.Unscoped().Where("flag_key = ?", testFlagKey).Delete(&model.FeatureFlag{}).Error; err != nil {
		t.Fatalf("既存テストフラグの削除に失敗: %v", err)
	}
	flag := model.FeatureFlag{
		FlagKey:          testFlagKey,
		DefaultVariation: "inline",
		Variations:       strPtr(`{"inline":"inline","modal":"modal","page":"page"}`),
		Enabled:          true,
	}
	if err := gormDB.Create(&flag).Error; err != nil {
		t.Fatalf("テストフラグ作成に失敗: %v", err)
	}
	t.Cleanup(func() {
		gormDB.Unscoped().Where("feature_flag_id = ?", flag.ID).Delete(&model.FeatureFlagAuditLog{})
		gormDB.Unscoped().Where("id = ?", flag.ID).Delete(&model.FeatureFlag{})
	})

	repo := repository.NewFeatureFlag(gormDB)
	svc := NewFeatureFlagService(repo)

	if err := svc.Update(ctx, flag.ID, UpdateInput{
		Enabled:          true,
		DefaultVariation: "modal",
		ChangedBy:        "test-admin",
	}); err != nil {
		t.Fatalf("Update失敗: %v", err)
	}

	got, err := svc.Get(ctx, flag.ID)
	if err != nil {
		t.Fatalf("Get失敗: %v", err)
	}
	if got.DefaultVariation != "modal" {
		t.Errorf("DefaultVariation = %q, want %q", got.DefaultVariation, "modal")
	}
	if got.IsBooleanToggle() {
		t.Error("IsBooleanToggle() = true, want false(3値フラグのため)")
	}
}

func strPtr(s string) *string { return &s }
