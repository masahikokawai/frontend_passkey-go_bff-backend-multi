package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-resty/resty/v2"

	"github.com/masahikokawai/bff-gin/bff/internal/auth"
)

// TaskClientV1 は backend の v1(REST、意図的にN+1がある旧実装)を呼び出す
// CONTRACT.mdセクション5.1のJSON形状に対応する
type TaskClientV1 struct {
	http *resty.Client
}

func NewTaskClientV1(baseURL string) *TaskClientV1 {
	return &TaskClientV1{http: resty.New().SetBaseURL(baseURL)}
}

// v1のTask JSON表現(backendのレスポンスをそのままアンマーシャルする用)
type taskV1DTO struct {
	ID          uint64  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	FinishedOn  string  `json:"finished_on"`
	UserID      uint64  `json:"user_id"`
	Labels      []Label `json:"labels"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func (d taskV1DTO) toTask() Task {
	return Task{
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		Status:      d.Status,
		FinishedOn:  d.FinishedOn,
		Labels:      d.Labels,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

type taskV1ListResponse struct {
	Tasks []taskV1DTO `json:"tasks"`
	Total int64       `json:"total"`
}

type taskV1RequestBody struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	FinishedOn  string   `json:"finished_on"`
	LabelIDs    []uint64 `json:"label_ids"`
}

// backendErrorBody はbackendが返すエラーJSONの`error`キーだけを見るための最小構造体
// (backend/internal/handler/v1/render.go・resolveUserIDのuser_not_provisioned判定用)
type backendErrorBody struct {
	Error string `json:"error"`
}

// classifyStatus はbackendからのHTTPステータスを見て、リアクティブリフレッシュの
// 対象(401)かどうかをauth.ErrUpstreamUnauthorizedとして呼び出し元へ伝える
//
// 【実機検証で発覚した不具合】
// admin画面でログイン中のユーザーを削除すると、backendは
// 403 {"error":"user_not_provisioned"} を返すが、以前はここで一般的なエラーとして
// 扱われ、生のエラー文字列がそのままフロントへ表示され、セッションも破棄されなかった
//
// この場合は401と同じく「セッションを破棄してログイン画面へ戻す」のが正しい挙動のため、
// auth.ErrUserNotProvisionedとして区別する(Refresher.Do参照)
func classifyStatus(resp *resty.Response, err error) error {
	if err != nil {
		return fmt.Errorf("backend v1呼び出しに失敗しました: %w", err)
	}
	if resp.StatusCode() == http.StatusUnauthorized {
		return auth.ErrUpstreamUnauthorized
	}
	if resp.StatusCode() == http.StatusForbidden {
		var body backendErrorBody
		if jsonErr := json.Unmarshal(resp.Body(), &body); jsonErr == nil && body.Error == "user_not_provisioned" {
			return auth.ErrUserNotProvisioned
		}
	}
	if resp.IsError() {
		return fmt.Errorf("backend v1がエラーを返しました: status=%d body=%s", resp.StatusCode(), resp.String())
	}
	return nil
}

func (c *TaskClientV1) List(ctx context.Context, accessToken string, userID uint64, filter TaskFilter) (TaskListResult, error) {
	req := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		SetQueryParam("user_id", strconv.FormatUint(userID, 10)).
		SetQueryParam("limit", strconv.Itoa(filter.Limit)).
		SetQueryParam("offset", strconv.Itoa(filter.Offset))
	if filter.Name != "" {
		req.SetQueryParam("name", filter.Name)
	}
	if filter.Status != "" {
		req.SetQueryParam("status", filter.Status)
	}
	if filter.SortFinishedOn != "" {
		req.SetQueryParam("sort", filter.SortFinishedOn)
	}
	if len(filter.LabelIDs) > 0 {
		ids := make([]string, len(filter.LabelIDs))
		for i, id := range filter.LabelIDs {
			ids[i] = strconv.FormatUint(id, 10)
		}
		req.SetQueryParam("label_ids", strings.Join(ids, ","))
	}

	var out taskV1ListResponse
	resp, err := req.SetResult(&out).Get("/internal/v1/tasks")
	if err := classifyStatus(resp, err); err != nil {
		return TaskListResult{}, err
	}

	tasks := make([]Task, 0, len(out.Tasks))
	for _, t := range out.Tasks {
		tasks = append(tasks, t.toTask())
	}
	return TaskListResult{Tasks: tasks, Total: out.Total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (c *TaskClientV1) Create(ctx context.Context, accessToken string, userID uint64, input TaskInput) (Task, error) {
	var out taskV1DTO
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		SetQueryParam("user_id", strconv.FormatUint(userID, 10)).
		SetBody(taskV1RequestBody{Name: input.Name, Description: input.Description, Status: input.Status, FinishedOn: input.FinishedOn, LabelIDs: input.LabelIDs}).
		SetResult(&out).
		Post("/internal/v1/tasks")
	if err := classifyStatus(resp, err); err != nil {
		return Task{}, err
	}
	return out.toTask(), nil
}

func (c *TaskClientV1) Get(ctx context.Context, accessToken string, userID, id uint64) (Task, error) {
	var out taskV1DTO
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		SetQueryParam("user_id", strconv.FormatUint(userID, 10)).
		SetResult(&out).
		Get(fmt.Sprintf("/internal/v1/tasks/%d", id))
	if err := classifyStatus(resp, err); err != nil {
		return Task{}, err
	}
	return out.toTask(), nil
}

func (c *TaskClientV1) Update(ctx context.Context, accessToken string, userID, id uint64, input TaskInput) (Task, error) {
	var out taskV1DTO
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		SetQueryParam("user_id", strconv.FormatUint(userID, 10)).
		SetBody(taskV1RequestBody{Name: input.Name, Description: input.Description, Status: input.Status, FinishedOn: input.FinishedOn, LabelIDs: input.LabelIDs}).
		SetResult(&out).
		Patch(fmt.Sprintf("/internal/v1/tasks/%d", id))
	if err := classifyStatus(resp, err); err != nil {
		return Task{}, err
	}
	return out.toTask(), nil
}

func (c *TaskClientV1) Delete(ctx context.Context, accessToken string, userID, id uint64) error {
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		SetQueryParam("user_id", strconv.FormatUint(userID, 10)).
		Delete(fmt.Sprintf("/internal/v1/tasks/%d", id))
	return classifyStatus(resp, err)
}
