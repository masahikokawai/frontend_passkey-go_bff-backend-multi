// Package proxy はBFFがbackendへリクエストを転送する層
// CONTRACT.mdセクション3: backendがv1(REST)/v2(gRPC)のどちらで応答しても、
// Reactへは常に同一形状のJSONを返す(BFFが差異を吸収する)
package proxy

import "context"

// Label はTaskに付与されるラベル
type Label struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// Task はBFF→ReactのTask表現
// CONTRACT.mdセクション3のJSON形状に対応する
type Task struct {
	ID          uint64  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	FinishedOn  string  `json:"finishedOn"`
	Labels      []Label `json:"labels"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

// TaskListResult は一覧APIの結果
type TaskListResult struct {
	Tasks  []Task `json:"tasks"`
	Total  int64  `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// TaskFilter は一覧取得時の絞り込み条件
// v1(REST)/v2(gRPC)共通のBFF内部表現
type TaskFilter struct {
	Name           string
	Status         string
	LabelIDs       []uint64
	SortFinishedOn string
	Limit          int
	Offset         int
}

// TaskInput はTaskの作成・更新に使う入力
type TaskInput struct {
	Name        string
	Description string
	Status      string
	FinishedOn  string
	LabelIDs    []uint64
}

// TaskBackendClient はv1(REST)/v2(gRPC)いずれのクライアントも実装するinterface
// task_route.goはこのinterfaceだけを見て、どちらの実装が来ても同じ扱いができる
// accessTokenは呼び出しごとに引数として渡す(auth.Refresherがリフレッシュ後の
// 新しいトークンで同じ呼び出しを再実行できるようにするため、クライアント内部に
// トークンを保持させない設計)
type TaskBackendClient interface {
	List(ctx context.Context, accessToken string, userID uint64, filter TaskFilter) (TaskListResult, error)
	Create(ctx context.Context, accessToken string, userID uint64, input TaskInput) (Task, error)
	Get(ctx context.Context, accessToken string, userID, id uint64) (Task, error)
	Update(ctx context.Context, accessToken string, userID, id uint64, input TaskInput) (Task, error)
	Delete(ctx context.Context, accessToken string, userID, id uint64) error
}
