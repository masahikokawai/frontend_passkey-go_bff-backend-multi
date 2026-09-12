package com.bffgin.backendpekko.grpc

import com.bffgin.backendpekko._
import com.bffgin.backendpekko.auth.JwtDispatcher
import com.google.protobuf.timestamp.{Timestamp => PBTimestamp}
import io.grpc.Status
import org.apache.pekko.grpc.GrpcServiceException
import org.apache.pekko.grpc.scaladsl.Metadata
// task.v1.TaskService(生成された素のトレイト)と com.bffgin.backendpekko.TaskService
// (このプロジェクトの業務ロジッククラス)が名前衝突するため、前者だけ除外してimportする
import task.v1.{TaskService => _, _}

import java.time.{Instant, LocalDate}
import scala.concurrent.{ExecutionContext, Future}
import scala.util.{Failure, Success, Try}

// backend/internal/grpcserver/task_service.go(gRPC v2)に対応するpekko-grpc実装
// server_power_apis生成設定により、各RPCメソッドがMetadataを受け取れる(Authorizationヘッダを
// ここから取り出す。Go実装のinterceptor.goがmetadataから"authorization"を読むのと同じ)
final class TaskGrpcServiceImpl(taskService: TaskService, dispatcher: JwtDispatcher)(implicit ec: ExecutionContext)
    extends TaskServicePowerApi {

  private def resolveUserId(metadata: Metadata): Future[Long] = {
    val tokenOpt = metadata.getText("authorization").map(_.stripPrefix("Bearer ")).filter(_.nonEmpty)
    tokenOpt match {
      case None => Future.failed(new GrpcServiceException(Status.UNAUTHENTICATED.withDescription("unauthorized")))
      case Some(token) =>
        dispatcher.verify(token) match {
          case Left(_) =>
            Future.failed(new GrpcServiceException(Status.UNAUTHENTICATED.withDescription("unauthorized")))
          case Right(claims) =>
            taskService.resolveUserId(claims).flatMap {
              case Right(id) => Future.successful(id)
              case Left(ServiceError.UserNotProvisioned) =>
                Future.failed(new GrpcServiceException(Status.PERMISSION_DENIED.withDescription("user not provisioned")))
              case Left(_) =>
                Future.failed(new GrpcServiceException(Status.INTERNAL))
            }
        }
    }
  }

  private def toGrpcError(err: ServiceError): GrpcServiceException = err match {
    case ServiceError.NotFound           => new GrpcServiceException(Status.NOT_FOUND.withDescription("task not found"))
    case ServiceError.Validation(msg)    => new GrpcServiceException(Status.INVALID_ARGUMENT.withDescription(msg))
    case ServiceError.UserNotProvisioned => new GrpcServiceException(Status.PERMISSION_DENIED.withDescription("user not provisioned"))
  }

  private def toPBTimestamp(i: Instant): PBTimestamp = PBTimestamp(i.getEpochSecond, i.getNano)

  private def toPBTask(dto: TaskDTO): Task =
    Task(
      id = dto.id,
      name = dto.name,
      description = dto.description,
      status = dto.status,
      finishedOn = dto.finishedOn.toString,
      labels = dto.labels.map(l => Label(id = l.id, name = l.name)),
      createdAt = Some(toPBTimestamp(dto.createdAt)),
      updatedAt = Some(toPBTimestamp(dto.updatedAt))
    )

  private def parseFinishedOn(s: String): Either[GrpcServiceException, LocalDate] =
    Try(LocalDate.parse(s)) match {
      case Success(d) => Right(d)
      case Failure(_) => Left(new GrpcServiceException(Status.INVALID_ARGUMENT.withDescription(s"invalid finished_on: $s")))
    }

  override def listTasks(in: ListTasksRequest, metadata: Metadata): Future[ListTasksResponse] =
    resolveUserId(metadata).flatMap { userId =>
      val status = if (in.status.nonEmpty) Some(in.status) else None
      status match {
        case Some(s) if !TaskStatus.isValid(s) =>
          Future.failed(new GrpcServiceException(Status.INVALID_ARGUMENT.withDescription(s"invalid status: $s")))
        case _ =>
          val name = if (in.name.nonEmpty) Some(in.name) else None
          val limit = if (in.limit > 0) in.limit else 20
          taskService.listCursor(userId, name, status, in.labelIds, in.cursor, limit).map { case (dtos, nextCursor) =>
            ListTasksResponse(tasks = dtos.map(toPBTask), nextCursor = nextCursor)
          }
      }
    }

  override def getTask(in: GetTaskRequest, metadata: Metadata): Future[Task] =
    resolveUserId(metadata).flatMap { userId =>
      taskService.get(in.id, userId).flatMap {
        case Right(dto) => Future.successful(toPBTask(dto))
        case Left(_)    => Future.failed(new GrpcServiceException(Status.NOT_FOUND.withDescription("task not found")))
      }
    }

  override def createTask(in: CreateTaskRequest, metadata: Metadata): Future[Task] =
    resolveUserId(metadata).flatMap { userId =>
      parseFinishedOn(in.finishedOn) match {
        case Left(e) => Future.failed(e)
        case Right(date) =>
          val input = TaskInput(in.name, in.description, in.status, date, in.labelIds)
          taskService.create(userId, input).flatMap {
            case Right(dto) => Future.successful(toPBTask(dto))
            case Left(err)  => Future.failed(toGrpcError(err))
          }
      }
    }

  override def updateTask(in: UpdateTaskRequest, metadata: Metadata): Future[Task] =
    resolveUserId(metadata).flatMap { userId =>
      parseFinishedOn(in.finishedOn) match {
        case Left(e) => Future.failed(e)
        case Right(date) =>
          val input = TaskInput(in.name, in.description, in.status, date, in.labelIds)
          taskService.update(in.id, userId, input).flatMap {
            case Right(dto) => Future.successful(toPBTask(dto))
            case Left(err)  => Future.failed(toGrpcError(err))
          }
      }
    }

  override def deleteTask(in: DeleteTaskRequest, metadata: Metadata): Future[DeleteTaskResponse] =
    resolveUserId(metadata).flatMap { userId =>
      taskService.delete(in.id, userId).flatMap {
        case Right(_)  => Future.successful(DeleteTaskResponse())
        case Left(err) => Future.failed(toGrpcError(err))
      }
    }
}
