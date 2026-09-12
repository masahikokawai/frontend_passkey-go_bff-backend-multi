// Package grpcserver はgRPC v2(新実装)のサービス実装
//
// 【重要】
// このファイルは `buf generate`(backend/buf.gen.yaml)で
// backend/gen/task/v1 配下に protoc-gen-go / protoc-gen-go-grpc の生成コードが作られていることを前提にしている
//
// このサンドボックス環境にはGoツールチェーンと protoc プラグインが無いため生成できておらず、生成前はこのファイルはビルドできない
// README 記載の手順で `buf generate` を実行してから `go build ./...` すること
package grpcserver

import (
	"context"
	"errors"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskv1 "github.com/masahikokawai/training-go/bff-gin/backend/gen/task/v1"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// TaskServer はtaskv1.TaskServiceServerインタフェースの実装
type TaskServer struct {
	taskv1.UnimplementedTaskServiceServer

	tasks *service.TaskService
	users *repository.User
}

func NewTaskServer(tasks *service.TaskService, users *repository.User) *TaskServer {
	return &TaskServer{tasks: tasks, users: users}
}

// resolveUserID はinterceptorが検証済みのJWT `sub` から内部ユーザーIDを引く
// BFFが申告するIDを信頼せず、必ずここで引き直す(CONTRACT.md参照)
//
// 【セクション16.5で変更】v1/task.goのresolveUserIDと同じ理由で、ローカル
// (HMAC/RSA)発行のJWTとKeycloak発行のJWTでsubの意味が異なるため分岐する
func (s *TaskServer) resolveUserID(ctx context.Context) (uint64, error) {
	claims, ok := authjwt.ClaimsFromGRPCContext(ctx)
	if !ok {
		return 0, status.Error(codes.Unauthenticated, "unauthorized")
	}

	if authjwt.IsLocalIssuer(claims.Issuer) {
		id, err := strconv.ParseUint(claims.Subject, 10, 64)
		if err != nil {
			return 0, status.Error(codes.PermissionDenied, "user not provisioned")
		}
		user, err := s.users.Get(ctx, id)
		if err != nil {
			return 0, status.Error(codes.PermissionDenied, "user not provisioned")
		}
		return user.ID, nil
	}

	user, err := s.users.GetByKeycloakSub(ctx, claims.Subject)
	if err != nil {
		return 0, status.Error(codes.PermissionDenied, "user not provisioned")
	}
	return user.ID, nil
}

func (s *TaskServer) ListTasks(ctx context.Context, req *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, error) {
	userID, err := s.resolveUserID(ctx)
	if err != nil {
		return nil, err
	}

	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 20
	}

	var status_ *model.TaskStatus
	if req.GetStatus() != "" {
		st, err := model.TaskStatusFromString(req.GetStatus())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid status: %v", err)
		}
		status_ = &st
	}

	labelIDs := make([]uint64, 0, len(req.GetLabelIds()))
	labelIDs = append(labelIDs, req.GetLabelIds()...)

	dtos, nextCursor, err := s.tasks.ListOptimized(ctx, userID, service.TaskListFilterV2{
		Name:     req.GetName(),
		Status:   status_,
		LabelIDs: labelIDs,
		Cursor:   req.GetCursor(),
		Limit:    limit,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list tasks: %v", err)
	}

	pbTasks := make([]*taskv1.Task, 0, len(dtos))
	for _, dto := range dtos {
		pbTasks = append(pbTasks, toPBTask(dto))
	}
	return &taskv1.ListTasksResponse{Tasks: pbTasks, NextCursor: nextCursor}, nil
}

func (s *TaskServer) GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.Task, error) {
	userID, err := s.resolveUserID(ctx)
	if err != nil {
		return nil, err
	}
	dto, err := s.tasks.Get(ctx, req.GetId(), userID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	return toPBTask(dto), nil
}

func (s *TaskServer) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.Task, error) {
	userID, err := s.resolveUserID(ctx)
	if err != nil {
		return nil, err
	}
	finishedOn, err := time.Parse("2006-01-02", req.GetFinishedOn())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid finished_on: %v", err)
	}
	dto, err := s.tasks.Create(ctx, userID, service.TaskInput{
		Name:        req.GetName(),
		Description: req.Description,
		Status:      req.GetStatus(),
		FinishedOn:  finishedOn,
		LabelIDs:    req.GetLabelIds(),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}
	return toPBTask(dto), nil
}

func (s *TaskServer) UpdateTask(ctx context.Context, req *taskv1.UpdateTaskRequest) (*taskv1.Task, error) {
	userID, err := s.resolveUserID(ctx)
	if err != nil {
		return nil, err
	}
	finishedOn, err := time.Parse("2006-01-02", req.GetFinishedOn())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid finished_on: %v", err)
	}
	dto, err := s.tasks.Update(ctx, req.GetId(), userID, service.TaskInput{
		Name:        req.GetName(),
		Description: req.Description,
		Status:      req.GetStatus(),
		FinishedOn:  finishedOn,
		LabelIDs:    req.GetLabelIds(),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}
	return toPBTask(dto), nil
}

func (s *TaskServer) DeleteTask(ctx context.Context, req *taskv1.DeleteTaskRequest) (*taskv1.DeleteTaskResponse, error) {
	userID, err := s.resolveUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.tasks.Delete(ctx, req.GetId(), userID); err != nil {
		return nil, toGRPCError(err)
	}
	return &taskv1.DeleteTaskResponse{}, nil
}

func toGRPCError(err error) error {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, service.ErrValidation):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Errorf(codes.Internal, "%v", err)
	}
}

func toPBTask(dto service.TaskDTO) *taskv1.Task {
	labels := make([]*taskv1.Label, 0, len(dto.Labels))
	for _, l := range dto.Labels {
		labels = append(labels, &taskv1.Label{Id: l.ID, Name: l.Name})
	}
	return &taskv1.Task{
		Id:          dto.ID,
		Name:        dto.Name,
		Description: dto.Description,
		Status:      dto.Status,
		FinishedOn:  dto.FinishedOn.Format("2006-01-02"),
		Labels:      labels,
		CreatedAt:   timestamppb.New(dto.CreatedAt),
		UpdatedAt:   timestamppb.New(dto.UpdatedAt),
	}
}
