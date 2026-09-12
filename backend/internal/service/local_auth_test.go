package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/crypto/bcrypt"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

type fakeLocalAuthRepo struct {
	user *model.User
	pw   *model.UserPassword
}

func (f *fakeLocalAuthRepo) GetByEmail(ctx context.Context, email string) (*model.User, *model.UserPassword, error) {
	if f.user == nil || f.user.Email != email {
		return nil, nil, errors.New("record not found")
	}
	return f.user, f.pw, nil
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcryptハッシュ生成に失敗: %v", err)
	}
	return string(h)
}

func TestLocalAuthService_VerifyLocalPassword(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	newService := func(repo *fakeLocalAuthRepo) *LocalAuthService {
		s := NewLocalAuthService(repo)
		s.now = func() time.Time { return fixedNow }
		return s
	}

	t.Run("正しいemail/passwordかつ有効期限内なら成功する", func(t *testing.T) {
		repo := &fakeLocalAuthRepo{
			user: &model.User{ID: 1, Name: "ローカル太郎", Email: "local@example.com", Role: model.RoleGeneral},
			pw: &model.UserPassword{
				UserID:            1,
				PasswordDigest:    mustHash(t, "password"),
				PasswordExpiresAt: fixedNow.Add(24 * time.Hour),
			},
		}
		s := newService(repo)

		got, err := s.VerifyLocalPassword(ctx, "local@example.com", "password")
		if err != nil {
			t.Fatalf("VerifyLocalPassword失敗: %v", err)
		}
		want := LocalAuthResult{UserID: 1, Name: "ローカル太郎", Email: "local@example.com", Roles: []string{"general"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("emailが存在しないとErrInvalidCredentials", func(t *testing.T) {
		repo := &fakeLocalAuthRepo{}
		s := newService(repo)

		_, err := s.VerifyLocalPassword(ctx, "not-found@example.com", "password")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("err = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("パスワードが違うとErrInvalidCredentials", func(t *testing.T) {
		repo := &fakeLocalAuthRepo{
			user: &model.User{ID: 1, Email: "local@example.com"},
			pw: &model.UserPassword{
				UserID:            1,
				PasswordDigest:    mustHash(t, "password"),
				PasswordExpiresAt: fixedNow.Add(24 * time.Hour),
			},
		}
		s := newService(repo)

		_, err := s.VerifyLocalPassword(ctx, "local@example.com", "wrong-password")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("err = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("パスワードは正しいが有効期限切れならErrPasswordExpired", func(t *testing.T) {
		repo := &fakeLocalAuthRepo{
			user: &model.User{ID: 1, Email: "local@example.com"},
			pw: &model.UserPassword{
				UserID:            1,
				PasswordDigest:    mustHash(t, "password"),
				PasswordExpiresAt: fixedNow.Add(-time.Hour),
			},
		}
		s := newService(repo)

		_, err := s.VerifyLocalPassword(ctx, "local@example.com", "password")
		if !errors.Is(err, ErrPasswordExpired) {
			t.Fatalf("err = %v, want ErrPasswordExpired", err)
		}
	})

	t.Run("有効期限がちょうど現在時刻と同じなら期限切れ扱いにしない(time.After境界)", func(t *testing.T) {
		// 実装は s.now().After(pw.PasswordExpiresAt) で判定している
		// 現在時刻と有効期限がぴったり同じ瞬間はAfter=falseなので、まだ有効期限内として扱う
		// (「その瞬間ちょうどまでは有効」という直感的な境界と一致することを確認する)
		repo := &fakeLocalAuthRepo{
			user: &model.User{ID: 1, Email: "local@example.com"},
			pw: &model.UserPassword{
				UserID:            1,
				PasswordDigest:    mustHash(t, "password"),
				PasswordExpiresAt: fixedNow,
			},
		}
		s := newService(repo)

		_, err := s.VerifyLocalPassword(ctx, "local@example.com", "password")
		if err != nil {
			t.Fatalf("有効期限ちょうどはまだ有効なはずだが失敗した: %v", err)
		}
	})

	t.Run("management roleはRolesにmanagementとして反映される", func(t *testing.T) {
		repo := &fakeLocalAuthRepo{
			user: &model.User{ID: 2, Email: "admin@example.com", Role: model.RoleManagement},
			pw: &model.UserPassword{
				UserID:            2,
				PasswordDigest:    mustHash(t, "password"),
				PasswordExpiresAt: fixedNow.Add(24 * time.Hour),
			},
		}
		s := newService(repo)

		got, err := s.VerifyLocalPassword(ctx, "admin@example.com", "password")
		if err != nil {
			t.Fatalf("VerifyLocalPassword失敗: %v", err)
		}
		if len(got.Roles) != 1 || got.Roles[0] != "management" {
			t.Errorf("Roles = %v, want [management]", got.Roles)
		}
	})
}
