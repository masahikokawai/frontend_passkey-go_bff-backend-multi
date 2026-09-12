package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// WebauthnCredential はwebauthn_credentialsテーブルへのアクセスを担当する
// CONTRACT.md セクション22参照
type WebauthnCredential struct {
	db *gorm.DB
}

func NewWebauthnCredential(db *gorm.DB) *WebauthnCredential {
	return &WebauthnCredential{db: db}
}

// Create は新しいパスキーを登録する
func (r *WebauthnCredential) Create(ctx context.Context, cred *model.WebauthnCredential) error {
	if err := r.db.WithContext(ctx).Create(cred).Error; err != nil {
		return fmt.Errorf("パスキーの登録に失敗しました: %w", err)
	}
	return nil
}

// FindByCredentialID はcredential_id(グローバルに一意)から1件引く
// 見つからない場合はgorm.ErrRecordNotFoundをそのまま返す(service層でservice.ErrNotFoundへ変換する、
// internal/repository/user.goと同じ方針)
func (r *WebauthnCredential) FindByCredentialID(ctx context.Context, credentialID []byte) (*model.WebauthnCredential, error) {
	var cred model.WebauthnCredential
	if err := r.db.WithContext(ctx).Where("credential_id = ?", credentialID).First(&cred).Error; err != nil {
		return nil, err
	}
	return &cred, nil
}

// UpdateSignCount はログイン成功後にsign_countを更新する(リプレイ攻撃対策)
// 該当行が無ければgorm.ErrRecordNotFoundを返す
func (r *WebauthnCredential) UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint64) error {
	res := r.db.WithContext(ctx).Model(&model.WebauthnCredential{}).
		Where("credential_id = ?", credentialID).
		Update("sign_count", signCount)
	if res.Error != nil {
		return fmt.Errorf("sign_countの更新に失敗しました: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UserIDsWithPasskey は少なくとも1件パスキーを登録済みのuser_idの集合を返す
// Admin::Usersの一覧に`has_passkey`を出すため(CONTRACT.mdセクション22.4)
func (r *WebauthnCredential) UserIDsWithPasskey(ctx context.Context) (map[uint64]bool, error) {
	var userIDs []uint64
	if err := r.db.WithContext(ctx).Model(&model.WebauthnCredential{}).
		Distinct("user_id").Pluck("user_id", &userIDs).Error; err != nil {
		return nil, fmt.Errorf("パスキー登録状況の取得に失敗しました: %w", err)
	}
	set := make(map[uint64]bool, len(userIDs))
	for _, id := range userIDs {
		set[id] = true
	}
	return set, nil
}
