package model

import "time"

// UserPassword は user_passwords テーブルの写し
// CONTRACT.md セクション16.2/16.3: ローカル(非Keycloak)認証を使うユーザーの
// bcryptハッシュ化済みパスワードと、その有効期限を保持する
// Railsの元設計における UserCredential(パスワード版、has_secure_password)に相当する
type UserPassword struct {
	ID                uint64    `gorm:"column:id;primaryKey"`
	UserID            uint64    `gorm:"column:user_id"`
	PasswordDigest    string    `gorm:"column:password_digest"`
	PasswordExpiresAt time.Time `gorm:"column:password_expires_at"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (UserPassword) TableName() string {
	return "user_passwords"
}
