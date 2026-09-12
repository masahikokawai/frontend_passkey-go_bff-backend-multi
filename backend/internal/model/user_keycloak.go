package model

import "time"

// UserKeycloak は user_keycloaks テーブルの写し
// CONTRACT.md セクション16.2: Keycloak経由でログインするユーザーの認証情報
// (JWTの`sub`クレームと一致するkeycloak_sub)を、usersから分離して保持する
// Railsの元設計における UserCredential(Keycloak版)に相当する
type UserKeycloak struct {
	ID          uint64    `gorm:"column:id;primaryKey"`
	UserID      uint64    `gorm:"column:user_id"`
	KeycloakSub string    `gorm:"column:keycloak_sub"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (UserKeycloak) TableName() string {
	return "user_keycloaks"
}
