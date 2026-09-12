package com.bffgin.backend.grpc

import io.grpc._
import org.slf4j.LoggerFactory

import java.util.concurrent.TimeUnit

// gRPCのリクエスト単位のログを出すinterceptor。REST(TaskRoutes)・外部公開API(ExternalTaskRoutes)は
// http4sの標準ミドルウェアLogger.httpAppで対応済みだが(Main.scala参照)、grpc-java(fs2-grpcの下層)には
// 同等の組み込みミドルウェアが無いため、標準的なServerInterceptorパターン
// (ServerCall#closeをラップしてstatusを捕捉する)で自前実装する。REST/外部APIのログ追加時に
// 「fs2-grpcにはドロップイン相当のミドルウェアが無い」として一旦見送った箇所を埋める。
//
// interceptCallはgrpc-javaの同期コールバックAPIのため、log4cats(IO)ではなくSLF4Jを直接使う
// (log4catsのLogger[IO]をここで呼ぶにはunsafeRunSyncが必要になり、かえって複雑になるため)
final class LoggingServerInterceptor extends ServerInterceptor {
  private val logger = LoggerFactory.getLogger("grpc.request")

  override def interceptCall[ReqT, RespT](
      call: ServerCall[ReqT, RespT],
      headers: Metadata,
      next: ServerCallHandler[ReqT, RespT]
  ): ServerCall.Listener[ReqT] = {
    val method = call.getMethodDescriptor.getFullMethodName
    val start = System.nanoTime()
    val wrappedCall = new ForwardingServerCall.SimpleForwardingServerCall[ReqT, RespT](call) {
      override def close(status: Status, trailers: Metadata): Unit = {
        val durationMs = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - start)
        logger.info(s"method=$method status=${status.getCode.name()} duration_ms=$durationMs")
        super.close(status, trailers)
      }
    }
    next.startCall(wrappedCall, headers)
  }
}
