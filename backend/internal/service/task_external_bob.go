// task_external_bob.go はCONTRACT.mdセクション24: Feature Flag `backend.external-tasks-orm`が
// "bob"のときに使う実装
//
// task_external.go(GORM版、ListExternalV1/ListExternalV2)と処理内容はほぼ同じだが、
// あえて別関数として複製している。理由は「既存2パターン(gorm)の挙動は一切変えないこと」という
// 要件を、コードの共通化より優先したため(page/pageSize/limitのデフォルト値決定・
// cursorのencode/decodeといった数行の重複は許容し、GORM版のコードには一切触れない
// ことでリグレッションのリスクをゼロにする判断)
//
// DTOへの変換(toTaskDTO)・cursorのencode/decode(encodeExternalCursor/decodeExternalCursor)は
// GORM版・bob版のどちらで取得したmodel.Taskに対しても同じロジックが使えるため、
// task_external.goの関数をそのまま再利用している(repository層で戻り値の型を
// model.Taskに揃えているからこそ、この再利用ができる)
package service

import (
	"context"
	"fmt"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// ListExternalV1Bob はListExternalV1のbob版(offsetページング)
func (s *TaskService) ListExternalV1Bob(ctx context.Context, filter ExternalListFilterV1) ([]TaskDTO, int64, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	tasks, total, err := s.repo.ListOffsetForExternalAPIBob(ctx, filter.UserID, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("外部API v1一覧取得(bob): %w", err)
	}
	dtos := make([]TaskDTO, 0, len(tasks))
	for _, t := range tasks {
		dtos = append(dtos, toTaskDTO(t))
	}
	return dtos, total, nil
}

// ListExternalV2Bob はListExternalV2のbob版(keyset・cursorページング)
func (s *TaskService) ListExternalV2Bob(ctx context.Context, filter ExternalListFilterV2) ([]TaskDTO, string, error) {
	limit := filter.Limit
	if limit < 1 {
		limit = 10
	}

	var after *repository.TaskCursor
	if filter.Cursor != "" {
		decoded, err := decodeExternalCursor(filter.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("%w: cursorの形式が不正です", ErrValidation)
		}
		after = decoded
	}

	tasks, err := s.repo.ListCursorForExternalAPIBob(ctx, filter.UserID, after, limit)
	if err != nil {
		return nil, "", fmt.Errorf("外部API v2一覧取得(bob): %w", err)
	}

	dtos := make([]TaskDTO, 0, len(tasks))
	for _, t := range tasks {
		dtos = append(dtos, toTaskDTO(t))
	}

	var nextCursor string
	if len(tasks) == limit {
		last := tasks[len(tasks)-1]
		nextCursor = encodeExternalCursor(last.CreatedAt, last.ID)
	}
	return dtos, nextCursor, nil
}
