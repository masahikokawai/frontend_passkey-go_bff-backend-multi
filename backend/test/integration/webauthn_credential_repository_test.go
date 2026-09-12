//go:build integration

// repository.WebauthnCredential(CONTRACT.mdセクション22)を実MySQLで検証する
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
)

func TestWebauthnCredentialRepository(t *testing.T) {
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
	credRepo := repository.NewWebauthnCredential(gormDB)
	ctx := context.Background()

	const testEmail = "webauthn-cred-repo-test@example.com"
	cleanup := func() {
		gormDB.Unscoped().Exec(
			"DELETE FROM webauthn_credentials WHERE user_id IN (SELECT id FROM (SELECT id FROM users WHERE email = ?) AS u)", testEmail)
		gormDB.Unscoped().Exec(
			"DELETE FROM user_passwords WHERE user_id IN (SELECT id FROM (SELECT id FROM users WHERE email = ?) AS u)", testEmail)
		gormDB.Unscoped().Where("email = ?", testEmail).Delete(&model.User{})
	}
	cleanup()
	t.Cleanup(cleanup)

	user, err := userRepo.Create(ctx, "パスキーテスト太郎", testEmail, "dummy-digest", model.RoleGeneral, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}

	name := "iPhone"
	cred := &model.WebauthnCredential{
		UserID:       user.ID,
		CredentialID: []byte("integration-test-credential-id"),
		PublicKey:    []byte("integration-test-public-key"),
		SignCount:    0,
		Name:         &name,
	}

	t.Run("Create: 登録できる", func(t *testing.T) {
		if err := credRepo.Create(ctx, cred); err != nil {
			t.Fatalf("Create失敗: %v", err)
		}
		if cred.ID == 0 {
			t.Fatal("IDが採番されていない")
		}
	})

	t.Run("FindByCredentialID: 登録した内容が引ける", func(t *testing.T) {
		got, err := credRepo.FindByCredentialID(ctx, []byte("integration-test-credential-id"))
		if err != nil {
			t.Fatalf("FindByCredentialID失敗: %v", err)
		}
		if got.UserID != user.ID {
			t.Errorf("UserID = %d, want %d", got.UserID, user.ID)
		}
		if string(got.PublicKey) != "integration-test-public-key" {
			t.Errorf("PublicKey = %q, want integration-test-public-key", got.PublicKey)
		}
	})

	t.Run("UpdateSignCount: sign_countが更新される", func(t *testing.T) {
		if err := credRepo.UpdateSignCount(ctx, []byte("integration-test-credential-id"), 5); err != nil {
			t.Fatalf("UpdateSignCount失敗: %v", err)
		}
		got, err := credRepo.FindByCredentialID(ctx, []byte("integration-test-credential-id"))
		if err != nil {
			t.Fatalf("FindByCredentialID失敗: %v", err)
		}
		if got.SignCount != 5 {
			t.Errorf("SignCount = %d, want 5", got.SignCount)
		}
	})

	t.Run("UserIDsWithPasskey: 登録済みユーザーのIDが含まれる", func(t *testing.T) {
		set, err := credRepo.UserIDsWithPasskey(ctx)
		if err != nil {
			t.Fatalf("UserIDsWithPasskey失敗: %v", err)
		}
		if !set[user.ID] {
			t.Errorf("user_id=%d がパスキー登録済みの集合に含まれていない", user.ID)
		}
	})

	// 【テスト監査で追加】1人のユーザーが複数の端末でパスキーを登録するケース
	// (例: iPhoneとMacの両方に登録)を想定し、UserIDsWithPasskeyがDISTINCT user_idで
	// 正しく1件に集約されること(2行あってもクエリがエラーにならず、mapのkeyとしては
	// 当然重複しないこと自体は自明だが、"2行存在する状態でのクエリ実行"自体を
	// 検証していなかった)を確認する
	t.Run("UserIDsWithPasskey: 同じユーザーが2つ目のパスキーを登録しても集合は変わらない", func(t *testing.T) {
		second := &model.WebauthnCredential{
			UserID:       user.ID,
			CredentialID: []byte("integration-test-credential-id-2"),
			PublicKey:    []byte("integration-test-public-key-2"),
		}
		if err := credRepo.Create(ctx, second); err != nil {
			t.Fatalf("2つ目のCreate失敗: %v", err)
		}
		set, err := credRepo.UserIDsWithPasskey(ctx)
		if err != nil {
			t.Fatalf("UserIDsWithPasskey失敗: %v", err)
		}
		if !set[user.ID] {
			t.Errorf("2つ目のパスキー登録後もuser_id=%dが含まれているべき", user.ID)
		}
	})

	t.Run("重複するcredential_idはUNIQUE制約でエラーになる", func(t *testing.T) {
		dup := &model.WebauthnCredential{
			UserID:       user.ID,
			CredentialID: []byte("integration-test-credential-id"),
			PublicKey:    []byte("other-key"),
		}
		if err := credRepo.Create(ctx, dup); err == nil {
			t.Fatal("credential_id重複なのにエラーが返らなかった")
		}
	})
}
