// Package service はFeature Flag管理のユースケース(更新+監査ログ記録)を担う
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/repository"
)

type FeatureFlagService struct {
	repo *repository.FeatureFlag
}

func NewFeatureFlagService(repo *repository.FeatureFlag) *FeatureFlagService {
	return &FeatureFlagService{repo: repo}
}

func (s *FeatureFlagService) List(ctx context.Context) ([]model.FeatureFlag, error) {
	return s.repo.List(ctx)
}

func (s *FeatureFlagService) Get(ctx context.Context, id uint64) (*model.FeatureFlag, error) {
	return s.repo.Get(ctx, id)
}

// UpdateInput は編集フォームからの入力
type UpdateInput struct {
	Enabled          bool
	DefaultVariation string
	ChangedBy        string
}

// Update はフラグを更新し、変更前後の値を監査ログへ1行残す
// Rails対比: コントローラでの `flag.update!` の代わりに、こちらは
// 「更新」と「監査ログ作成」を1つのユースケースとして明示的にサービス層へ切り出している
// (Railsの`after_save`コールバックで暗黙にログを作るより、変更点が呼び出し元から見えやすい)
func (s *FeatureFlagService) Update(ctx context.Context, id uint64, in UpdateInput) error {
	flag, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}

	// CONTRACT.mdセクション19: 以前は"on"/"off"の2値を決め打ちで検証していたが、
	// 多値(multivariate)フラグに対応するため、そのフラグ自身のVariationsに
	// 定義されているキーのいずれかであることを検証する形へ一般化した
	options := flag.VariationOptions()
	valid := false
	for _, opt := range options {
		if opt == in.DefaultVariation {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("default_variationは %v のいずれかである必要があります: got=%q", options, in.DefaultVariation)
	}

	beforeVariation := flag.DefaultVariation
	beforeEnabled := flag.Enabled

	flag.DefaultVariation = in.DefaultVariation
	flag.Enabled = in.Enabled

	auditLog := &model.FeatureFlagAuditLog{
		FeatureFlagID:          flag.ID,
		FlagKey:                flag.FlagKey,
		BeforeDefaultVariation: &beforeVariation,
		AfterDefaultVariation:  in.DefaultVariation,
		BeforeEnabled:          &beforeEnabled,
		AfterEnabled:           in.Enabled,
		ChangedBy:              in.ChangedBy,
		ChangedAt:              time.Now(),
	}

	return s.repo.UpdateWithAuditLog(ctx, flag, auditLog)
}

func (s *FeatureFlagService) AuditLogs(ctx context.Context, featureFlagID uint64) ([]model.FeatureFlagAuditLog, error) {
	return s.repo.ListAuditLogs(ctx, featureFlagID)
}
