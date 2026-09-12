// Package repository はfeature_flags/feature_flag_audit_logsテーブルへのアクセスを担う
package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/model"
)

type FeatureFlag struct {
	db *gorm.DB
}

func NewFeatureFlag(db *gorm.DB) *FeatureFlag {
	return &FeatureFlag{db: db}
}

// List はflag_key昇順で全件返す(一覧画面用)
func (r *FeatureFlag) List(ctx context.Context) ([]model.FeatureFlag, error) {
	var flags []model.FeatureFlag
	if err := r.db.WithContext(ctx).Order("flag_key ASC").Find(&flags).Error; err != nil {
		return nil, fmt.Errorf("フラグ一覧取得: %w", err)
	}
	return flags, nil
}

func (r *FeatureFlag) Get(ctx context.Context, id uint64) (*model.FeatureFlag, error) {
	var flag model.FeatureFlag
	if err := r.db.WithContext(ctx).First(&flag, id).Error; err != nil {
		return nil, fmt.Errorf("フラグ取得(id=%d): %w", id, err)
	}
	return &flag, nil
}

// UpdateWithAuditLog はフラグの更新と監査ログの追記を1トランザクションで行う
// Railsで言えば `ActiveRecord::Base.transaction do ... end` に相当する
func (r *FeatureFlag) UpdateWithAuditLog(ctx context.Context, flag *model.FeatureFlag, auditLog *model.FeatureFlagAuditLog) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(flag).Error; err != nil {
			return fmt.Errorf("フラグ更新(id=%d): %w", flag.ID, err)
		}
		if err := tx.Create(auditLog).Error; err != nil {
			return fmt.Errorf("監査ログ作成(feature_flag_id=%d): %w", auditLog.FeatureFlagID, err)
		}
		return nil
	})
}

// ListAuditLogs は指定フラグの変更履歴を新しい順で返す
func (r *FeatureFlag) ListAuditLogs(ctx context.Context, featureFlagID uint64) ([]model.FeatureFlagAuditLog, error) {
	var logs []model.FeatureFlagAuditLog
	if err := r.db.WithContext(ctx).
		Where("feature_flag_id = ?", featureFlagID).
		Order("changed_at DESC").
		Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("監査ログ一覧取得(feature_flag_id=%d): %w", featureFlagID, err)
	}
	return logs, nil
}
