// task_external.go はCONTRACT.mdセクション11(BFF非経由の外部公開API)専用のロジック
//
// task_v1.go(N+1あり)/task_v2.go(Preload+idのみのcursor)とは別の学習主題
// (offsetとcursorのページング性能特性の対比)に絞るため、あえてここに分離している
package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// ExternalListFilterV1 はoffsetページング(Feature Flag OFF、既定)
type ExternalListFilterV1 struct {
	UserID   uint64
	Page     int
	PageSize int
}

// ExternalListFilterV2 はkeyset(cursor)ページング(Feature Flag ON)
type ExternalListFilterV2 struct {
	UserID uint64
	Cursor string // 空文字なら先頭ページ
	Limit  int
}

// ListExternalV1 はoffsetベースの一覧取得
func (s *TaskService) ListExternalV1(ctx context.Context, filter ExternalListFilterV1) ([]TaskDTO, int64, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 10
	}

	tasks, total, err := s.repo.ListOffsetForExternalAPI(ctx, filter.UserID, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("外部API v1一覧取得: %w", err)
	}
	dtos := make([]TaskDTO, 0, len(tasks))
	for _, t := range tasks {
		dtos = append(dtos, toTaskDTO(t))
	}
	return dtos, total, nil
}

// ListExternalV2 は keyset(cursor)ベースの一覧取得
// 次ページが無ければ nextCursor="" を返す
func (s *TaskService) ListExternalV2(ctx context.Context, filter ExternalListFilterV2) ([]TaskDTO, string, error) {
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

	tasks, err := s.repo.ListCursorForExternalAPI(ctx, filter.UserID, after, limit)
	if err != nil {
		return nil, "", fmt.Errorf("外部API v2一覧取得: %w", err)
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

// encodeExternalCursor/decodeExternalCursor は (created_at, id) を base64 エンコードした不透明な文字列として往復させる
//
// クライアントに「idの連番であること」等の内部実装を意識させないための opaque cursor(オペークカーソル)という定石
// 中身は "<RFC3339Nano>|<id>" をbase64url化しただけの単純なもの
func encodeExternalCursor(createdAt time.Time, id uint64) string {
	raw := fmt.Sprintf("%s|%d", createdAt.Format(time.RFC3339Nano), id)
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

func decodeExternalCursor(cursor string) (*repository.TaskCursor, error) {
	raw, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("base64デコード失敗: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("cursorの区切りが不正です")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, fmt.Errorf("created_atの形式が不正です: %w", err)
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("idの形式が不正です: %w", err)
	}
	return &repository.TaskCursor{CreatedAt: createdAt, ID: id}, nil
}
