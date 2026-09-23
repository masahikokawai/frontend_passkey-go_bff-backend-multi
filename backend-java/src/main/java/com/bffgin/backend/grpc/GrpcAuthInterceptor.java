package com.bffgin.backend.grpc;

import io.grpc.Context;
import io.grpc.Contexts;
import io.grpc.Metadata;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;

/**
 * "authorization"メタデータを読み取り、gRPCのContextへ格納するだけの薄いインターセプタ。
 * BFFからはAuthorizationヘッダ(REST)/authorization metadata(gRPC)でaccess tokenを転送する
 * (CONTRACT.mdセクション5)
 */
public final class GrpcAuthInterceptor implements ServerInterceptor {

    static final Metadata.Key<String> AUTHORIZATION_KEY =
            Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER);
    static final Context.Key<String> AUTHORIZATION_CONTEXT_KEY = Context.key("authorization");

    @Override
    public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
            ServerCall<ReqT, RespT> call, Metadata headers, ServerCallHandler<ReqT, RespT> next) {
        String authHeader = headers.get(AUTHORIZATION_KEY);
        Context context = Context.current().withValue(AUTHORIZATION_CONTEXT_KEY, authHeader);
        return Contexts.interceptCall(context, call, headers, next);
    }
}
