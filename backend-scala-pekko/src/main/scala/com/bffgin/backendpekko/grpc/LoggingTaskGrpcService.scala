package com.bffgin.backendpekko.grpc

import org.apache.pekko.grpc.GrpcServiceException
import org.apache.pekko.grpc.scaladsl.Metadata
import org.slf4j.Logger
import task.v1._

import scala.concurrent.{ExecutionContext, Future}
import scala.util.{Failure, Success}

// TaskGrpcServiceImpl(型付きの業務ロジック実装)をそのままラップし、実際のgRPCステータス
// (io.grpc.Status.Code、成功時はOK)をmethod/durationと一緒にログする。
//
// 【ベストプラクティスの見直しで採用】当初はMain.scalaのHTTPトランスポート層
// (TaskServicePowerApiHandler.partialが返すPartialFunction)をラップしていたが、gRPCの
// 成否はgrpc-statusトレーラーにしか現れず、外側のHTTPレスポンスは常に200にしかならないため
// 正確な成否が取れなかった(method/durationのみのログに留まっていた)。
// TaskGrpcServiceImplの各メソッドは失敗時に必ずFuture.failed(new GrpcServiceException(status))を
// 返す(実装を確認済み)ため、ここでラップする方がGo(grpc.ChainUnaryInterceptor)・
// Scala(http4s)(io.grpc.ServerInterceptor)と同じ精度で、かつHTTP/2トレーラーを
// 覗き見るような低レベルな実装を一切必要としない
final class LoggingTaskGrpcService(delegate: TaskServicePowerApi, logger: Logger)(implicit ec: ExecutionContext)
    extends TaskServicePowerApi {

  private def logged[T](rpcName: String)(fut: Future[T]): Future[T] = {
    val start = System.nanoTime()
    fut.andThen {
      case Success(_) =>
        val durationMs = (System.nanoTime() - start) / 1000000L
        logger.info(s"method=$rpcName status=OK duration_ms=$durationMs")
      case Failure(e) =>
        val durationMs = (System.nanoTime() - start) / 1000000L
        // GrpcServiceExceptionはio.grpc.StatusRuntimeExceptionのサブクラスで、実際に
        // クライアントへ返るio.grpc.Statusをそのまま持っている。それ以外の(本来
        // 起きないはずの)予期しない例外はUNKNOWNとして記録する
        val statusName = e match {
          case grpcEx: GrpcServiceException => grpcEx.status.getCode.name()
          case _                            => "UNKNOWN"
        }
        logger.info(s"method=$rpcName status=$statusName duration_ms=$durationMs")
    }
  }

  override def listTasks(in: ListTasksRequest, metadata: Metadata): Future[ListTasksResponse] =
    logged("listTasks")(delegate.listTasks(in, metadata))

  override def getTask(in: GetTaskRequest, metadata: Metadata): Future[Task] =
    logged("getTask")(delegate.getTask(in, metadata))

  override def createTask(in: CreateTaskRequest, metadata: Metadata): Future[Task] =
    logged("createTask")(delegate.createTask(in, metadata))

  override def updateTask(in: UpdateTaskRequest, metadata: Metadata): Future[Task] =
    logged("updateTask")(delegate.updateTask(in, metadata))

  override def deleteTask(in: DeleteTaskRequest, metadata: Metadata): Future[DeleteTaskResponse] =
    logged("deleteTask")(delegate.deleteTask(in, metadata))
}
