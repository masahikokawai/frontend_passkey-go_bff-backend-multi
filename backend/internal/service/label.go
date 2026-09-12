package service

import (
	"context"
	"fmt"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// labelRepository はLabelServiceが必要とする最小のリポジトリ操作
// (テストでfakeに差し替えられるよう、具象型ではなくinterfaceに依存する
// task.goのwebauthnRepository等と同じ方針)
type labelRepository interface {
	List(ctx context.Context) ([]model.Label, error)
	Get(ctx context.Context, id uint64) (*model.Label, error)
	Create(ctx context.Context, label *model.Label) error
	Update(ctx context.Context, label *model.Label) error
	Delete(ctx context.Context, id uint64) error
	CountTaskLabels(ctx context.Context, labelID uint64) (int64, error)
}

// LabelService はLabel CRUD(v1 RESTのみ、gRPC化の対象外、CONTRACT.md参照)
type LabelService struct {
	repo labelRepository
}

func NewLabelService(repo *repository.Label) *LabelService {
	return &LabelService{repo: repo}
}

func toLabelDTO(l model.Label) LabelDTO {
	return LabelDTO{ID: l.ID, Name: l.Name}
}

func (s *LabelService) List(ctx context.Context) ([]LabelDTO, error) {
	labels, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	dtos := make([]LabelDTO, 0, len(labels))
	for _, l := range labels {
		dtos = append(dtos, toLabelDTO(l))
	}
	return dtos, nil
}

// validateLabelName はRails版のバリデーション(name必須・一意・10文字以内)を再現する
// task.goのvalidateTaskInputと同様、DBに触れない純粋な関数として切り出すことで、
// リポジトリ(実DB接続)無しに単体テストできるようにしている
func validateLabelName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: nameは必須です", ErrValidation)
	}
	if len([]rune(name)) > 10 {
		return fmt.Errorf("%w: nameは10文字以内である必要があります", ErrValidation)
	}
	return nil
}

// Create はRails版のバリデーション(name必須・一意・10文字以内)を再現する
func (s *LabelService) Create(ctx context.Context, name string) (LabelDTO, error) {
	if err := validateLabelName(name); err != nil {
		return LabelDTO{}, err
	}
	label := &model.Label{Name: name}
	if err := s.repo.Create(ctx, label); err != nil {
		return LabelDTO{}, err
	}
	return toLabelDTO(*label), nil
}

func (s *LabelService) Update(ctx context.Context, id uint64, name string) (LabelDTO, error) {
	if err := validateLabelName(name); err != nil {
		return LabelDTO{}, err
	}
	label, err := s.repo.Get(ctx, id)
	if err != nil {
		return LabelDTO{}, fmt.Errorf("%w", ErrNotFound)
	}
	label.Name = name
	if err := s.repo.Update(ctx, label); err != nil {
		return LabelDTO{}, err
	}
	return toLabelDTO(*label), nil
}

// Delete はラベルを削除する
//
// 【3回目のテスト監査で発覚・修正】
// タスクに紐付いたまま削除できてしまうと、task_labelsに孤立行が残る
// (repository.Label.CountTaskLabelsのコメント参照)
// 「最後の管理者ガード」(UserService)と同じ、業務ルールとしての削除拒否パターンを踏襲する
func (s *LabelService) Delete(ctx context.Context, id uint64) error {
	count, err := s.repo.CountTaskLabels(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: このラベルは%d件のタスクに紐付いているため削除できません", ErrValidation, count)
	}
	return s.repo.Delete(ctx, id)
}
