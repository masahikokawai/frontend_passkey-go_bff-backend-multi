package grpcserver

import (
	"context"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	taskv1 "github.com/masahikokawai/training-go/bff-gin/backend/gen/task/v1"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
)

// loggingUnaryInterceptor は全rpcをmethod/status/durationでログする
// internal/handler/v1/router.go・internal/handler/external/router.goのrequestLoggerと
// 同じ考え方(REST側はこれが既にあったが、gRPC側には無く、実際にどのrpcを処理したか
// ログから確認できなかった。実機検証で判明・追記)
//
// authInterceptorより外側に置く(チェーンの先頭)ことで、認可エラー
// (codes.Unauthenticated等)で拒否したrpcもログに残す
func loggingUnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logger.Info("request",
			slog.String("method", info.FullMethod),
			slog.String("status", status.Code(err).String()),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
		return resp, err
	}
}

// New はgRPCサーバーを組み立てる(ログ・JWT検証interceptorを全rpcに適用)
func New(verifier authjwt.TokenVerifier, taskServer *TaskServer, logger *slog.Logger) *grpc.Server {
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			loggingUnaryInterceptor(logger),
			authjwt.UnaryServerInterceptor(verifier),
		),
	)
	taskv1.RegisterTaskServiceServer(srv, taskServer)
	return srv
}

// Listen はgRPCサーバーを指定アドレスで起動する(呼び出し元でgoroutine化する想定)
func Listen(srv *grpc.Server, addr string) (net.Listener, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return lis, nil
}
