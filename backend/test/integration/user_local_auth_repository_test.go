//go:build integration

// repository.User.GetByEmail(CONTRACT.mdセクション16.3)が、usersテーブルから
// keycloak_subカラムを除去しuser_passwords/user_keycloaksへ分離した後の実スキーマ
// (migrations/000008_split_user_credentials)に対して正しくJOINできることを、実 MySQL で検証する
// service/local_auth_test.goはfakeLocalAuthRepoでの単体テストのため、実際のGORM/SQLの往復はここでしか確認できない
// 実行前提: 環境変数 TEST_DB_DSN(README参照)
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
)

func TestUserRepository_GetByEmail(t *testing.T) {
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

	// 固定emailを使う(再実行の度に重複INSERTでMySQL error 1062にならないよう、実行前に前回分を掃除しておく
	// CONTRACT.md参照先: task_external_pagination_test.goと同じ
	// 「テストは何度実行しても同じ結果になる」ための後始末)
	const testEmail = "get-by-email-repo-test@example.com"
	if err := gormDB.Unscoped().Exec(
		"DELETE FROM user_passwords WHERE user_id IN (SELECT id FROM (SELECT id FROM users WHERE email = ?) AS u)", testEmail,
	).Error; err != nil {
		t.Fatalf("前回テストデータの後始末(user_passwords)に失敗: %v", err)
	}
	if err := gormDB.Unscoped().Where("email = ?", testEmail).Delete(&model.User{}).Error; err != nil {
		t.Fatalf("前回テストデータの後始末(users)に失敗: %v", err)
	}

	user := model.User{Email: testEmail, Name: "リポジトリテスト", Role: model.RoleGeneral}
	if err := gormDB.Create(&user).Error; err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	expiresAt := time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
	pw := model.UserPassword{UserID: user.ID, PasswordDigest: "dummy-digest", PasswordExpiresAt: expiresAt}
	if err := gormDB.Create(&pw).Error; err != nil {
		t.Fatalf("テストパスワード行作成失敗: %v", err)
	}

	t.Run("存在するemailならuser/user_passwordsの両方が引ける", func(t *testing.T) {
		gotUser, gotPw, err := userRepo.GetByEmail(context.Background(), user.Email)
		if err != nil {
			t.Fatalf("GetByEmail() error = %v", err)
		}
		if gotUser.ID != user.ID || gotUser.Name != user.Name {
			t.Errorf("gotUser = %+v, want ID=%d Name=%q", gotUser, user.ID, user.Name)
		}
		if gotPw.PasswordDigest != "dummy-digest" {
			t.Errorf("gotPw.PasswordDigest = %q, want dummy-digest", gotPw.PasswordDigest)
		}
		if diff := cmp.Diff(expiresAt, gotPw.PasswordExpiresAt.UTC()); diff != "" {
			t.Errorf("PasswordExpiresAtが往復で一致しない(-want +got):\n%s", diff)
		}
	})

	t.Run("存在しないemailはエラー", func(t *testing.T) {
		if _, _, err := userRepo.GetByEmail(context.Background(), "no-such-user@example.com"); err == nil {
			t.Error("エラーになるべきだが成功した")
		}
	})

	t.Run("migrations/000009でseedされたローカルユーザーがそのまま引ける(seedの回帰テスト)", func(t *testing.T) {
		gotUser, gotPw, err := userRepo.GetByEmail(context.Background(), "local-user@example.com")
		if err != nil {
			t.Fatalf("seedユーザーのGetByEmail() error = %v(migrations/000009が未適用の可能性)", err)
		}
		if gotUser.Name == "" {
			t.Error("seedユーザーのNameが空")
		}
		if !gotPw.PasswordExpiresAt.After(time.Now().AddDate(50, 0, 0)) {
			t.Errorf("seedユーザーのPasswordExpiresAt = %v, want 十分先(2099年)の日付", gotPw.PasswordExpiresAt)
		}
	})
}
