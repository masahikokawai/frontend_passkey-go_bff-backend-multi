package proxy

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/masahikokawai/bff-gin/bff/internal/auth"
	taskv1 "github.com/masahikokawai/bff-gin/bff/proto/task/v1"
)

// TaskClientV2 は backend の v2(gRPC、Preloadで最適化した新実装)を呼び出す
// v1がoffsetベースのページングなのに対し、v2はcursor(task.IDそのもの)ベースを
// 採用しているため(CONTRACT.md参照)、List内でoffset→cursorの変換を行う
//
// 統合レビューでの修正点: backend/proto/task/v1/task.proto(正)とbff側の.protoが
// 独立実装のため food違っていたため、backendの契約に合わせて全面的に書き直した
//   - user_id はリクエストに含めない(backendがJWT subから自己解決するため、
//     このprotoにはそもそもuser_idフィールドが無い)
//   - cursor は string ではなく uint64(直前ページ最後のtask.ID、0=先頭 / 次ページ無し)
//   - ListTasksResponse に total は無いため、正確な総件数は返せない(下記Total参照)
//   - ListTasksRequest に sort_finished_on は無い(backend側の既知の制約:
//     v2のcursorページングは常にid昇順固定で、finished_onソートの併用は非対応)
type TaskClientV2 struct {
	client taskv1.TaskServiceClient
}

// NewTaskClientV2 はgRPCの接続を張る
// TLSは private network 内での通信のため学習用途では insecure(平文)とする
// (本番ではmTLS等を検討する)
func NewTaskClientV2(addr string) (*TaskClientV2, func() error, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("backend v2(gRPC)への接続に失敗しました(addr=%s): %w", addr, err)
	}
	return &TaskClientV2{client: taskv1.NewTaskServiceClient(conn)}, conn.Close, nil
}

// withToken は access token を gRPC の metadata へ載せる
// REST の Authorization ヘッダに相当
func withToken(ctx context.Context, accessToken string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+accessToken)
}

// classifyGRPCErr はgRPCのstatus codeを見て、リアクティブリフレッシュの対象
// (Unauthenticated)かどうかをauth.ErrUpstreamUnauthorizedとして呼び出し元へ伝える
//
// 【実機検証で発覚した不具合】REST v1のclassifyStatusと同じ理由(task_client_v1.go参照)
// gRPC v2側でもbackendはresolveUserID失敗時にcodes.PermissionDenied("user not provisioned")を
// 返すため、REST v1と対称的に分類する
func classifyGRPCErr(err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.Unauthenticated {
			return auth.ErrUpstreamUnauthorized
		}
		if st.Code() == codes.PermissionDenied && st.Message() == "user not provisioned" {
			return auth.ErrUserNotProvisioned
		}
	}
	return fmt.Errorf("backend v2(gRPC)呼び出しに失敗しました: %w", err)
}

func fromPBTask(t *taskv1.Task) Task {
	labels := make([]Label, 0, len(t.GetLabels()))
	for _, l := range t.GetLabels() {
		labels = append(labels, Label{ID: l.GetId(), Name: l.GetName()})
	}
	// description は proto3 の optional string なので、生成型はそのまま *string
	// nilなら「未入力」、非nilなら値ありをそのまま表現できる(v1のようなhas_description
	// フラグを別途持つ必要がない)
	return Task{
		ID:          t.GetId(),
		Name:        t.GetName(),
		Description: t.Description,
		Status:      t.GetStatus(),
		FinishedOn:  t.GetFinishedOn(),
		Labels:      labels,
		CreatedAt:   t.GetCreatedAt().AsTime().Format(time.RFC3339),
		UpdatedAt:   t.GetUpdatedAt().AsTime().Format(time.RFC3339),
	}
}

func descriptionPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// List はoffset/limitのリクエストをcursor(task.ID)ベースのListTasksへ変換する
//
// cursorページングは「任意のoffsetへ一気に飛ぶ」ことを想定しない設計のため
// (実務でも「次へ」ボタンのみのUIに倒すのが一般的)
//
// ここでは学習目的の簡略実装として、
// offset分だけ内部的にページを読み進めてから limit 件を返す
// 件数が多い一覧で offset が大きい場合は非効率
// (実務の cursor 設計では offset によるランダムアクセスをそもそも提供しない)
// この一手間こそが「offsetベースの v1 から cursor ベースの v2 へ移行する際、BFFが両者の意味論の違いを吸収する」
//
// Total: backendのListTasksResponseにtotal件数は含まれない(cursorページングでは
// 「全件数を数える」こと自体が本来避けたいコスト)
// 正確な値を返せないため、呼び出し元(React)がtotalを表示に使っていないことを確認した上で -1(不明)を返す
func (c *TaskClientV2) List(ctx context.Context, accessToken string, userID uint64, filter TaskFilter) (TaskListResult, error) {
	ctx = withToken(ctx, accessToken)

	labelIDs := make([]uint64, len(filter.LabelIDs))
	copy(labelIDs, filter.LabelIDs)

	var cursor uint64 // 0 = 先頭から
	remaining := filter.Offset
	// 暴走防止
	// 極端に大きいoffsetは打ち切る
	const maxHops = 50
	for hop := 0; remaining > 0; hop++ {
		if hop >= maxHops {
			return TaskListResult{}, fmt.Errorf("offset(%d)が大きすぎてcursorベースのv2では現実的に辿れません", filter.Offset)
		}
		step := remaining
		if step > 100 {
			step = 100
		}
		resp, err := c.client.ListTasks(ctx, &taskv1.ListTasksRequest{
			Name: filter.Name, Status: filter.Status, LabelIds: labelIDs,
			Cursor: cursor, Limit: int32(step),
		})
		if err != nil {
			return TaskListResult{}, classifyGRPCErr(err)
		}
		if resp.GetNextCursor() == 0 {
			// offsetが総件数を超えている
			// 空リストとして扱う
			return TaskListResult{Tasks: []Task{}, Total: -1, Limit: filter.Limit, Offset: filter.Offset}, nil
		}
		cursor = resp.GetNextCursor()
		remaining -= step
	}

	resp, err := c.client.ListTasks(ctx, &taskv1.ListTasksRequest{
		Name: filter.Name, Status: filter.Status, LabelIds: labelIDs,
		Cursor: cursor, Limit: int32(filter.Limit),
	})
	if err != nil {
		return TaskListResult{}, classifyGRPCErr(err)
	}

	tasks := make([]Task, 0, len(resp.GetTasks()))
	for _, t := range resp.GetTasks() {
		tasks = append(tasks, fromPBTask(t))
	}
	return TaskListResult{Tasks: tasks, Total: -1, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (c *TaskClientV2) Create(ctx context.Context, accessToken string, userID uint64, input TaskInput) (Task, error) {
	ctx = withToken(ctx, accessToken)
	t, err := c.client.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Name: input.Name, Description: descriptionPtr(input.Description),
		Status: input.Status, FinishedOn: input.FinishedOn, LabelIds: input.LabelIDs,
	})
	if err != nil {
		return Task{}, classifyGRPCErr(err)
	}
	return fromPBTask(t), nil
}

func (c *TaskClientV2) Get(ctx context.Context, accessToken string, userID, id uint64) (Task, error) {
	ctx = withToken(ctx, accessToken)
	t, err := c.client.GetTask(ctx, &taskv1.GetTaskRequest{Id: id})
	if err != nil {
		return Task{}, classifyGRPCErr(err)
	}
	return fromPBTask(t), nil
}

func (c *TaskClientV2) Update(ctx context.Context, accessToken string, userID, id uint64, input TaskInput) (Task, error) {
	ctx = withToken(ctx, accessToken)
	t, err := c.client.UpdateTask(ctx, &taskv1.UpdateTaskRequest{
		Id: id, Name: input.Name, Description: descriptionPtr(input.Description),
		Status: input.Status, FinishedOn: input.FinishedOn, LabelIds: input.LabelIDs,
	})
	if err != nil {
		return Task{}, classifyGRPCErr(err)
	}
	return fromPBTask(t), nil
}

func (c *TaskClientV2) Delete(ctx context.Context, accessToken string, userID, id uint64) error {
	ctx = withToken(ctx, accessToken)
	_, err := c.client.DeleteTask(ctx, &taskv1.DeleteTaskRequest{Id: id})
	return classifyGRPCErr(err)
}
