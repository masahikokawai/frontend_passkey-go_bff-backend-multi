package com.bffgin.backend.grpc

import cats.effect.IO
import cats.syntax.all._
import io.grpc.{Metadata, Status, StatusException}
import task.v1.task.{
  Task => PbTask,
  Label => PbLabel,
  ListTasksRequest,
  ListTasksResponse,
  GetTaskRequest,
  CreateTaskRequest,
  UpdateTaskRequest,
  DeleteTaskRequest,
  DeleteTaskResponse,
  TaskServiceFs2Grpc
}
import com.bffgin.backend._
import com.bffgin.backend.auth.JwtAuth
import com.google.protobuf.timestamp.Timestamp
import java.time.ZoneOffset

// backend/internal/grpcserver/task_service.go のgRPC v2契約をそのまま再現する
// (CONTRACT.mdセクション5・20.5、proto定義はbackend/proto/task/v1/task.protoのコピーをそのまま使う)
// scalapbは "package task.v1;" + ファイル名"task.proto" から Scalaパッケージ task.v1.task を
// 生成する(flat_packageを指定していないデフォルト挙動)ため、生成メッセージはtask.v1.task.*にある
class TaskGrpcServiceImpl(service: TaskService, repo: TaskRepo, auth: JwtAuth) extends TaskServiceFs2Grpc[IO, Metadata] {

  private val authKey = Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER)

  private def authenticate(ctx: Metadata): IO[Long] =
    Option(ctx.get(authKey)) match {
      case None                                => IO.raiseError(new StatusException(Status.UNAUTHENTICATED.withDescription("unauthorized")))
      case Some(h) if !h.startsWith("Bearer ") => IO.raiseError(new StatusException(Status.UNAUTHENTICATED.withDescription("unauthorized")))
      case Some(h) =>
        val token = h.stripPrefix("Bearer ")
        auth
          .verify(token)
          .handleErrorWith(_ => IO.raiseError(new StatusException(Status.UNAUTHENTICATED.withDescription("invalid_token"))))
          .flatMap { claims =>
            UserResolver.resolve(claims, repo, auth).handleErrorWith {
              case AppError.UserNotProvisioned =>
                IO.raiseError(new StatusException(Status.PERMISSION_DENIED.withDescription("user not provisioned")))
              case e => IO.raiseError(e)
            }
          }
    }

  private def toGrpcError(e: Throwable): Throwable = e match {
    case AppError.NotFound               => new StatusException(Status.NOT_FOUND.withDescription("task not found"))
    case AppError.Validation(msg)        => new StatusException(Status.INVALID_ARGUMENT.withDescription(msg))
    case AppError.InvalidFinishedOn(raw) => new StatusException(Status.INVALID_ARGUMENT.withDescription(s"invalid finished_on: $raw"))
    case _: StatusException              => e
    case other                           => new StatusException(Status.INTERNAL.withDescription(other.getMessage))
  }

  private def wrapGrpcErrors[A](io: IO[A]): IO[A] = io.handleErrorWith(e => IO.raiseError(toGrpcError(e)))

  private def toPbTimestamp(ldt: java.time.LocalDateTime): Timestamp = {
    val instant = ldt.atOffset(ZoneOffset.UTC).toInstant
    Timestamp(seconds = instant.getEpochSecond, nanos = instant.getNano)
  }

  private def toPbTask(t: Task): PbTask =
    PbTask(
      id = t.id,
      name = t.name,
      description = t.description,
      status = t.status.wire,
      finishedOn = t.finishedOn.toString,
      labels = t.labels.map(l => PbLabel(id = l.id, name = l.name)),
      createdAt = Some(toPbTimestamp(t.createdAt)),
      updatedAt = Some(toPbTimestamp(t.updatedAt))
    )

  override def listTasks(request: ListTasksRequest, ctx: Metadata): IO[ListTasksResponse] =
    for {
      userId <- authenticate(ctx)
      statusOpt <- if (request.status.isEmpty) IO.pure(None)
                   else
                     TaskStatus.fromWire(request.status) match {
                       case Right(st) => IO.pure(Some(st))
                       case Left(msg) => IO.raiseError(new StatusException(Status.INVALID_ARGUMENT.withDescription(s"invalid status: $msg")))
                     }
      limit = if (request.limit <= 0) 20 else request.limit
      result <- wrapGrpcErrors(service.listCursor(userId, request.name, statusOpt, request.labelIds.toList, request.cursor, limit))
      (tasks, nextCursor) = result
    } yield ListTasksResponse(tasks = tasks.map(toPbTask), nextCursor = nextCursor)

  override def getTask(request: GetTaskRequest, ctx: Metadata): IO[PbTask] =
    for {
      userId <- authenticate(ctx)
      t      <- service.get(request.id, userId).handleErrorWith(_ => IO.raiseError(new StatusException(Status.NOT_FOUND.withDescription("task not found"))))
    } yield toPbTask(t)

  override def createTask(request: CreateTaskRequest, ctx: Metadata): IO[PbTask] =
    for {
      userId <- authenticate(ctx)
      t <- wrapGrpcErrors(
        service.create(userId, TaskInput(request.name, request.description, request.status, request.finishedOn, request.labelIds.toList))
      )
    } yield toPbTask(t)

  override def updateTask(request: UpdateTaskRequest, ctx: Metadata): IO[PbTask] =
    for {
      userId <- authenticate(ctx)
      t <- wrapGrpcErrors(
        service.update(
          request.id,
          userId,
          TaskInput(request.name, request.description, request.status, request.finishedOn, request.labelIds.toList)
        )
      )
    } yield toPbTask(t)

  override def deleteTask(request: DeleteTaskRequest, ctx: Metadata): IO[DeleteTaskResponse] =
    for {
      userId <- authenticate(ctx)
      _      <- wrapGrpcErrors(service.delete(request.id, userId))
    } yield DeleteTaskResponse()
}
