package com.bffgin.backend.grpc

import io.grpc.Context
import io.grpc.Contexts
import io.grpc.Metadata
import io.grpc.ServerCall
import io.grpc.ServerCallHandler
import io.grpc.ServerInterceptor

/**
 * "authorization"メタデータを読み取り、gRPCのContextへ格納するだけの薄いインターセプタ。
 * BFFからはAuthorizationヘッダ(REST)/authorization metadata(gRPC)でaccess tokenを転送する
 * (CONTRACT.mdセクション5)。grpc-kotlinのcoroutine実装もgrpc-javaのContextをそのまま
 * 引き継ぐため(内部でContext.asContextElement()相当の伝播を行う)、Java実装と同じ設計で書ける
 */
class GrpcAuthInterceptor : ServerInterceptor {

    companion object {
        val AUTHORIZATION_KEY: Metadata.Key<String> = Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER)
        val AUTHORIZATION_CONTEXT_KEY: Context.Key<String> = Context.key("authorization")
    }

    override fun <ReqT, RespT> interceptCall(
        call: ServerCall<ReqT, RespT>,
        headers: Metadata,
        next: ServerCallHandler<ReqT, RespT>,
    ): ServerCall.Listener<ReqT> {
        val authHeader = headers.get(AUTHORIZATION_KEY)
        val context = Context.current().withValue(AUTHORIZATION_CONTEXT_KEY, authHeader)
        return Contexts.interceptCall(context, call, headers, next)
    }
}
