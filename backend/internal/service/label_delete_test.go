package service

import (
	"context"
	"errors"
	"testing"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// fakeLabelRepo は labelRepository の最小フェイク実装
// DB接続無しで LabelService.Delete の「使用中のラベルは削除できない」ロジックを検証するために使う
type fakeLabelRepo struct {
	taskLabelCount int64
	deleteCalled   bool
	deleteErr      error
}

func (f *fakeLabelRepo) List(context.Context) ([]model.Label, error) { return nil, nil }
func (f *fakeLabelRepo) Get(context.Context, uint64) (*model.Label, error) {
	return &model.Label{ID: 1, Name: "x"}, nil
}
func (f *fakeLabelRepo) Create(context.Context, *model.Label) error { return nil }
func (f *fakeLabelRepo) Update(context.Context, *model.Label) error { return nil }
func (f *fakeLabelRepo) Delete(_ context.Context, _ uint64) error {
	f.deleteCalled = true
	return f.deleteErr
}
func (f *fakeLabelRepo) CountTaskLabels(context.Context, uint64) (int64, error) {
	return f.taskLabelCount, nil
}

// 【3回目のテスト監査で追加】タスクに紐付いたまま削除できてしまうと、task_labelsに
// 孤立行が残る実バグを発見したため、その修正(使用中は削除拒否)を確認する
func TestLabelService_Delete_使用中のラベルは削除できない(t *testing.T) {
	repo := &fakeLabelRepo{taskLabelCount: 3}
	s := &LabelService{repo: repo}

	err := s.Delete(context.Background(), 1)

	if err == nil {
		t.Fatal("エラーを期待したがnilだった")
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("ErrValidationを期待したが違うエラーだった: %v", err)
	}
	if repo.deleteCalled {
		t.Error("使用中のラベルなのに repo.Delete が呼ばれてしまった")
	}
}

func TestLabelService_Delete_未使用のラベルは削除できる(t *testing.T) {
	repo := &fakeLabelRepo{taskLabelCount: 0}
	s := &LabelService{repo: repo}

	err := s.Delete(context.Background(), 1)

	if err != nil {
		t.Fatalf("エラー無しを期待したが: %v", err)
	}
	if !repo.deleteCalled {
		t.Error("未使用のラベルなのに repo.Delete が呼ばれなかった")
	}
}
