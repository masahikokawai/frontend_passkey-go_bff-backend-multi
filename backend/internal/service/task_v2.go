package service

import (
	"context"
	"fmt"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// TaskListFilterV2 はgRPC v2(新実装)向けの一覧フィルタ
// keyset(cursor)ページングを使う
type TaskListFilterV2 struct {
	Name     string
	Status   *model.TaskStatus
	LabelIDs []uint64
	Cursor   uint64 // 直前ページの最後のtask.ID。0なら先頭から
	Limit    int
}

// ListOptimized はv2(新実装、gRPC)の一覧取得
//
// repo.ListWithLabelsPreloaded が Preload("Labels") で一括取得するため、
// 一覧が何件でも SQL は「タスク本体1回 + ラベル一括1回」の計2回で済み、v1 の N+1 が解消されている
// ページングも offset ではなく id 基準の keyset(cursor) 方式にしており、
// 大量データでも OFFSET が大きくなるほど遅くなる問題を回避する設計にした(v1からのもう1つの改善点)
//
// 学習目的の単純化として、cursorページングは常に id 昇順固定
// finished_on ソート等の併用は本実装のスコープ外(README/TODOに明記)
func (s *TaskService) ListOptimized(ctx context.Context, userID uint64, filter TaskListFilterV2) ([]TaskDTO, uint64, error) {
	limit := filter.Limit
	tasks, err := s.repo.ListWithLabelsPreloaded(ctx, repository.TaskFilter{
		UserID:    userID,
		Name:      filter.Name,
		Status:    filter.Status,
		LabelIDs:  filter.LabelIDs,
		UseCursor: true,
		Cursor:    filter.Cursor,
		Limit:     limit,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("タスク一覧取得(v2): %w", err)
	}

	dtos := make([]TaskDTO, 0, len(tasks))
	var nextCursor uint64
	for _, t := range tasks {
		dtos = append(dtos, toTaskDTO(t))
		nextCursor = t.ID
	}
	if len(tasks) < limit {
		// このページで尽きた(次ページが無い)ことをBFF/呼び出し元へ伝える規約として0を返す
		nextCursor = 0
	}
	return dtos, nextCursor, nil
}
