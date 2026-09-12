package authjwt

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type claimsCtxKeyType struct{}

var claimsCtxKey = claimsCtxKeyType{}

// UnaryServerInterceptor はgRPC v2向けのJWT検証
// RequireAuth(Gin用) と同じ Verifier を共有し、REST/gRPC で検証ロジックが二重化しないようにしている
//
// Rails対比: Rails には gRPC という概念自体が無いため直接の対応物は無いが、
// 「同じ認証ロジックを複数のプロトコルアダプタから呼び出す」という発想は
// Rails の `before_action` をControllerの外に切り出すイメージに近い
func UnaryServerInterceptor(verifier TokenVerifier) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		claims, err := verifyFromMetadata(ctx, verifier)
		if err != nil {
			return nil, err
		}
		return handler(context.WithValue(ctx, claimsCtxKey, claims), req)
	}
}

func verifyFromMetadata(ctx context.Context, verifier TokenVerifier) (*Claims, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	token, ok := strings.CutPrefix(values[0], "Bearer ")
	if !ok || token == "" {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}

	claims, err := verifier.Verify(token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid_token")
	}
	return claims, nil
}

// ClaimsFromGRPCContext はgRPCサービス実装側でログイン中ユーザーの情報を取り出すヘルパー
func ClaimsFromGRPCContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsCtxKey).(*Claims)
	return claims, ok
}
