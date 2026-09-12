package service

import (
	"context"
	"fmt"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// TaskListFilterV1 はREST v1(旧実装)向けの一覧フィルタ(offsetページング)
type TaskListFilterV1 struct {
	Name           string
	Status         *model.TaskStatus
	LabelIDs       []uint64
	SortFinishedOn string
	Limit          int
	Offset         int
}

// ListLegacy はv1(旧実装、REST)の一覧取得
//
// 【意図的な非効率実装であることの注記】
// repo.ListWithoutLabels でPreloadせずにタスク本体だけをまとめて取得したあと、
// タスク1件ごとに repo.LabelsForTask を呼び出しており、これは典型的なN+1クエリである
// (一覧が20件なら「一覧取得1回 + ラベル取得20回」= 計21回のSQLが発行される)
// これは本番のベストプラクティスではなく、CONTRACT.mdで決めた学習用のビフォーアフター比較
// (v1=N+1 / v2=Preload)のためにあえて再現した旧実装である
//
// FIXME: 実務でこのコードを書いてはいけない
func (s *TaskService) ListLegacy(ctx context.Context, userID uint64, filter TaskListFilterV1) ([]TaskDTO, int64, error) {
	tasks, total, err := s.repo.ListWithoutLabels(ctx, repository.TaskFilter{
		UserID:         userID,
		Name:           filter.Name,
		Status:         filter.Status,
		LabelIDs:       filter.LabelIDs,
		SortFinishedOn: filter.SortFinishedOn,
		Limit:          filter.Limit,
		Offset:         filter.Offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("タスク一覧取得(v1): %w", err)
	}

	dtos := make([]TaskDTO, 0, len(tasks))
	for _, t := range tasks {
		// ↓↓↓ ここがN+1の発生箇所
		// FIXME: ループの中でクエリを1回ずつ発行している
		labels, err := s.repo.LabelsForTask(ctx, t.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("タスク(id=%d)のラベル取得(v1): %w", t.ID, err)
		}
		t.Labels = labels
		dtos = append(dtos, toTaskDTO(t))
	}
	return dtos, total, nil
}
