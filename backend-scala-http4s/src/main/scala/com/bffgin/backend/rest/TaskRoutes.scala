package com.bffgin.backend.rest

import cats.effect.IO
import cats.syntax.all._
import org.http4s._
import org.http4s.dsl.io._
import org.http4s.circe._
import io.circe.{Json => CJson}
import org.typelevel.ci.CIString
import com.bffgin.backend._
import com.bffgin.backend.auth.JwtAuth

// backend/internal/handler/v1/task.go・render.go のREST v1契約をそのまま再現する
// (CONTRACT.mdセクション5.1・20.5)
class TaskRoutes(service: TaskService, repo: TaskRepo, auth: JwtAuth) {
  private implicit val reqBodyDecoder: EntityDecoder[IO, Json.TaskRequestBody] = jsonOf[IO, Json.TaskRequestBody]

  private def json(status: Status, body: CJson): IO[Response[IO]] =
    IO.pure(Response[IO](status).withEntity(body))

  private def authenticate(req: Request[IO]): IO[Long] = {
    val headerOpt = req.headers.get(CIString("Authorization")).map(_.head.value)
    headerOpt match {
      case None                                => IO.raiseError(AppError.Unauthorized)
      case Some(h) if !h.startsWith("Bearer ") => IO.raiseError(AppError.Unauthorized)
      case Some(h) =>
        val token = h.stripPrefix("Bearer ")
        for {
          claims <- auth.verify(token)
          userId <- UserResolver.resolve(claims, repo, auth)
        } yield userId
    }
  }

  private def errorResponse(e: Throwable): IO[Response[IO]] = {
    val (code, errKey, message) = AppError.toRestStatus(e)
    val body = message.fold(Json.errorJson(errKey))(m => Json.errorJsonWithMessage(errKey, m))
    json(Status.fromInt(code).getOrElse(Status.InternalServerError), body)
  }

  private def parseLabelIds(raw: Option[String]): List[Long] =
    raw.toList.flatMap(_.split(",").toList).flatMap(_.trim.toLongOption)

  // Goの `binding:"required"` は空文字列も不正扱いにする(gin/validatorのrequiredタグの挙動)
  private def bodyToInput(b: Json.TaskRequestBody): IO[TaskInput] =
    (b.name.filter(_.nonEmpty), b.status.filter(_.nonEmpty), b.finished_on.filter(_.nonEmpty)) match {
      case (Some(name), Some(status), Some(finishedOn)) =>
        IO.pure(TaskInput(name, b.description, status, finishedOn, b.label_ids.getOrElse(Nil)))
      case _ => IO.raiseError(AppError.InvalidRequest("invalid_request"))
    }

  val routes: HttpRoutes[IO] = HttpRoutes.of[IO] {
    case req @ GET -> Root / "internal" / "v1" / "tasks" =>
      (for {
        userId <- authenticate(req)
        q = req.uri.query.params
        name = q.getOrElse("name", "")
        limit = q.get("limit").flatMap(_.toIntOption).getOrElse(20)
        offset = q.get("offset").flatMap(_.toIntOption).getOrElse(0)
        labelIds = parseLabelIds(q.get("label_ids"))
        statusOpt <- q.get("status").filter(_.nonEmpty) match {
          case None => IO.pure(None)
          case Some(s) =>
            TaskStatus.fromWire(s) match {
              case Right(st) => IO.pure(Some(st))
              case Left(_)   => IO.raiseError(AppError.InvalidStatus(s))
            }
        }
        result             <- service.list(userId, name, statusOpt, labelIds, limit, offset)
        (tasks, total)      = result
        resp               <- json(Status.Ok, Json.listResponse(tasks, total, limit, offset))
      } yield resp).handleErrorWith(errorResponse)

    case req @ GET -> Root / "internal" / "v1" / "tasks" / LongVar(id) =>
      (for {
        userId <- authenticate(req)
        task   <- service.get(id, userId)
        resp   <- json(Status.Ok, Json.taskEncoder(task))
      } yield resp).handleErrorWith(errorResponse)

    case req @ POST -> Root / "internal" / "v1" / "tasks" =>
      (for {
        userId <- authenticate(req)
        body   <- req.as[Json.TaskRequestBody].handleErrorWith(_ => IO.raiseError(AppError.InvalidRequest("invalid_request")))
        input  <- bodyToInput(body)
        task   <- service.create(userId, input)
        resp   <- json(Status.Created, Json.taskEncoder(task))
      } yield resp).handleErrorWith(errorResponse)

    case req @ PATCH -> Root / "internal" / "v1" / "tasks" / LongVar(id) =>
      (for {
        userId <- authenticate(req)
        body   <- req.as[Json.TaskRequestBody].handleErrorWith(_ => IO.raiseError(AppError.InvalidRequest("invalid_request")))
        input  <- bodyToInput(body)
        task   <- service.update(id, userId, input)
        resp   <- json(Status.Ok, Json.taskEncoder(task))
      } yield resp).handleErrorWith(errorResponse)

    case req @ DELETE -> Root / "internal" / "v1" / "tasks" / LongVar(id) =>
      (for {
        userId <- authenticate(req)
        _      <- service.delete(id, userId)
        resp   <- IO.pure(Response[IO](Status.NoContent))
      } yield resp).handleErrorWith(errorResponse)
  }
}
