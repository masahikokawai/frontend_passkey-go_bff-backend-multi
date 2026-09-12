package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// ErrInvalidCredentials はemailが存在しない、またはパスワードがbcrypt不一致の場合
// タイミング攻撃対策(存在有無を区別しない)は本来望ましいが、学習用途のスコープでは
// 簡略化し、エラー種別だけをinvalid_credentials/password_expiredの2つに分けている
// (CONTRACT.md セクション16.3)
var ErrInvalidCredentials = errors.New("invalid_credentials")

// ErrPasswordExpired は資格情報自体は正しいが password_expires_at を過ぎている場合
// この場合もセッションは発行しない
// (「そもそもログインさせない」CONTRACT.md セクション16.1確認事項)
var ErrPasswordExpired = errors.New("password_expired")

// localAuthRepository はLocalAuthServiceが必要とする最小のリポジトリ操作
type localAuthRepository interface {
	GetByEmail(ctx context.Context, email string) (*model.User, *model.UserPassword, error)
}

// LocalAuthService はローカル(非Keycloak)認証のパスワード検証ユースケース
// bffはDBに直接アクセスしない方針(CONTRACT.md冒頭)のため、この検証は
// backendの内部エンドポイント(handler/v1/local_auth.go)経由でのみ呼ばれる
type LocalAuthService struct {
	repo localAuthRepository
	// now はテストで現在時刻を固定するためのフック
	// 本番は time.Now
	now func() time.Time
}

func NewLocalAuthService(repo localAuthRepository) *LocalAuthService {
	return &LocalAuthService{repo: repo, now: time.Now}
}

// LocalAuthResult はパスワード検証成功時にbffへ返す最小限のユーザー情報
// bffはこれを元に自分でJWTを発行する(sub=UserID)
type LocalAuthResult struct {
	UserID uint64
	Name   string
	Email  string
	Roles  []string
}

// VerifyLocalPassword はemail/passwordを検証する
// bcrypt不一致・email不存在はいずれもErrInvalidCredentials(存在有無を返さない)、
// 資格情報自体は正しいが有効期限切れの場合はErrPasswordExpiredを返す
func (s *LocalAuthService) VerifyLocalPassword(ctx context.Context, email, password string) (LocalAuthResult, error) {
	user, pw, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		return LocalAuthResult{}, fmt.Errorf("%w", ErrInvalidCredentials)
	}
	if bcrypt.CompareHashAndPassword([]byte(pw.PasswordDigest), []byte(password)) != nil {
		return LocalAuthResult{}, fmt.Errorf("%w", ErrInvalidCredentials)
	}
	if s.now().After(pw.PasswordExpiresAt) {
		return LocalAuthResult{}, fmt.Errorf("%w", ErrPasswordExpired)
	}
	return LocalAuthResult{
		UserID: user.ID,
		Name:   user.Name,
		Email:  user.Email,
		Roles:  []string{user.Role.String()},
	}, nil
}
