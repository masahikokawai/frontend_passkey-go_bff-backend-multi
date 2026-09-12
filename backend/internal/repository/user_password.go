package repository

import (
	"context"
	"fmt"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// GetByEmail はローカル認証(CONTRACT.md セクション16.3)のログイン試行時に、emailからusers/user_passwordsの両方を引く
//
// bcrypt比較・有効期限比較は service 層(LocalAuthService)が行う(リポジトリ層はDB以外の判断をしない方針)
func (r *User) GetByEmail(ctx context.Context, email string) (*model.User, *model.UserPassword, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		return nil, nil, fmt.Errorf("ユーザー検索(email=%s): %w", email, err)
	}
	var pw model.UserPassword
	if err := r.db.WithContext(ctx).Where("user_id = ?", user.ID).First(&pw).Error; err != nil {
		return nil, nil, fmt.Errorf("パスワード情報検索(user_id=%d): %w", user.ID, err)
	}
	return &user, &pw, nil
}
