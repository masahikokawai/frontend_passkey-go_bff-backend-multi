package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// FeatureFlag はfeature_flagsテーブルへの読み取りアクセスを担当する
// 書き込み(admin CRUD)はadmin/go・admin/railsがそれぞれ直接MySQLへ行うため、
// backend側はGET /internal/v1/feature-flags/export(bffのHTTP retriever向け)と
// backend自身のMySQL retrieverが使う読み取り専用の口だけを持つ(CONTRACT.mdセクション13参照)
type FeatureFlag struct {
	db *gorm.DB
}

func NewFeatureFlag(db *gorm.DB) *FeatureFlag {
	return &FeatureFlag{db: db}
}

// List は全フラグを返す(exportエンドポイント・MySQL retrieverの両方から使う)
func (r *FeatureFlag) List(ctx context.Context) ([]model.FeatureFlag, error) {
	var flags []model.FeatureFlag
	if err := r.db.WithContext(ctx).Order("flag_key ASC").Find(&flags).Error; err != nil {
		return nil, fmt.Errorf("feature_flags一覧取得: %w", err)
	}
	return flags, nil
}
