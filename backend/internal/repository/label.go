package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// Label はlabelsテーブルへのアクセスを担当する
// v1(REST)のみで使う(gRPC化の対象外)
type Label struct {
	db *gorm.DB
}

func NewLabel(db *gorm.DB) *Label {
	return &Label{db: db}
}

func (r *Label) List(ctx context.Context) ([]model.Label, error) {
	var labels []model.Label
	if err := r.db.WithContext(ctx).Order("name ASC").Find(&labels).Error; err != nil {
		return nil, fmt.Errorf("ラベル一覧取得: %w", err)
	}
	return labels, nil
}

func (r *Label) Get(ctx context.Context, id uint64) (*model.Label, error) {
	var label model.Label
	if err := r.db.WithContext(ctx).First(&label, id).Error; err != nil {
		return nil, err
	}
	return &label, nil
}

func (r *Label) Create(ctx context.Context, label *model.Label) error {
	if err := r.db.WithContext(ctx).Create(label).Error; err != nil {
		return fmt.Errorf("ラベル作成: %w", err)
	}
	return nil
}

func (r *Label) Update(ctx context.Context, label *model.Label) error {
	if err := r.db.WithContext(ctx).Save(label).Error; err != nil {
		return fmt.Errorf("ラベル更新: %w", err)
	}
	return nil
}

func (r *Label) Delete(ctx context.Context, id uint64) error {
	if err := r.db.WithContext(ctx).Delete(&model.Label{}, id).Error; err != nil {
		return fmt.Errorf("ラベル削除: %w", err)
	}
	return nil
}

// CountTaskLabels はこのlabel_idを参照しているtask_labels行の件数を返す
//
// 【3回目のテスト監査(認可・データ整合性観点)で発覚】
// task_labelsにはFK制約が無く
// (migrations/000004、FindMissingLabelIDsのコメント参照)、LabelService.Deleteは
// この件数を一切確認せずlabelsの行を削除していたため、タスクに紐付いたまま
// ラベルを削除すると、task_labels側に「存在しないlabel_idを指す」孤立行が残っていた
// (FindMissingLabelIDsが防いでいたのは作成時の孤立化のみで、削除時の孤立化は未対策だった)
//
// GORMのPreload("Labels")は内部でINNER JOINするため、孤立した行は単に無かったことに
// なり例外は起きないが、タスクのラベルが利用者に見えなくなる「静かなデータ消失」になる
//
// service.LabelService.Deleteでこの件数を見てErrValidationを返すことで、
// 使用中のラベル削除そのものを防ぐ(admin/goのFindMissingLabelIDsと対になる、
// 作成時・削除時の両方で孤立行を防ぐ形にする)
func (r *Label) CountTaskLabels(ctx context.Context, labelID uint64) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Table("task_labels").Where("label_id = ?", labelID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("task_labels参照件数の確認: %w", err)
	}
	return count, nil
}
