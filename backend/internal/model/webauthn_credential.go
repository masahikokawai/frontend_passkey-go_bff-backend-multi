package model

import "time"

// WebauthnCredential は webauthn_credentials テーブルの写し
// CONTRACT.md セクション22: 既存ユーザー(ローカル認証)向けの追加認証手段(パスキー)
//
// credential_id はグローバルに一意なルックアップキー
// (discoverable credentialでのログイン時、ユーザーが未特定の状態でこのIDから逆引きする必要があるため)
type WebauthnCredential struct {
	ID     uint64 `gorm:"column:id;primaryKey"`
	UserID uint64 `gorm:"column:user_id"`
	// CredentialID はbase64urlデコード後の生バイト列
	CredentialID []byte `gorm:"column:credential_id"`
	// PublicKey はCOSE形式の公開鍵
	PublicKey []byte `gorm:"column:public_key"`
	SignCount uint64 `gorm:"column:sign_count"`
	// BackupEligible/BackupState はWebAuthnのBE/BSフラグ(実機デバッグで追加、
	// マイグレーション000015参照)。登録時に記録した値をログイン時の検証にも使い直さないと、
	// go-webauthnが「登録時と申告内容が矛盾している」として正当なログインを拒否してしまう
	BackupEligible bool `gorm:"column:backup_eligible"`
	BackupState    bool `gorm:"column:backup_state"`
	// Transports は "internal,hybrid" 等、カンマ区切り(NULL許容)
	Transports *string   `gorm:"column:transports"`
	Name       *string   `gorm:"column:name"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (WebauthnCredential) TableName() string {
	return "webauthn_credentials"
}
