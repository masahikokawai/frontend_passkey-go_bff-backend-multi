package com.bffgin.backend.external

import cats.effect.IO
import cats.syntax.all._
import org.http4s._
import org.http4s.dsl.io._
import org.http4s.circe._
import io.circe.{Json => CJson}
import org.typelevel.ci.CIString
import com.bffgin.backend._
import com.bffgin.backend.auth.JwtAuth

// backend/internal/handler/external/task.go・authjwt/external_middleware.go の契約をそのまま再現する
// CONTRACT.mdセクション11・20.7。BFFを経由しない、Client Credentials Grant認証の直接API
class ExternalTaskRoutes(repo: TaskRepo, auth: JwtAuth, flags: ExternalFlags, expectedClientId: String) {

  private def json(status: Status, body: CJson): IO[Response[IO]] =
    IO.pure(Response[IO](status).withEntity(body))

  private def errorResponse(e: Throwable): IO[Response[IO]] = {
    val (code, errKey, _) = AppError.toRestStatus(e)
    json(Status.fromInt(code).getOrElse(Status.InternalServerError), ExternalJson.errorJson(errKey))
  }

  // RequireExternalClientAuth相当: 通常のJWT検証(3issuer共通)を再利用しつつ、
  // azpクレームがexpectedClientId(既定"external-api-client")と一致するかだけ追加で見る
  private def authenticate(req: Request[IO]): IO[Unit] = {
    val headerOpt = req.headers.get(CIString("Authorization")).map(_.head.value)
    headerOpt match {
      case None                                => IO.raiseError(AppError.Unauthorized)
      case Some(h) if !h.startsWith("Bearer ") => IO.raiseError(AppError.Unauthorized)
      case Some(h) =>
        val token = h.stripPrefix("Bearer ")
        auth.verify(token).flatMap { claims =>
          if (claims.azp.contains(expectedClientId)) IO.unit
          else IO.raiseError(AppError.ClientNotAllowed)
        }
    }
  }

  private def parseUserID(q: Map[String, String]): IO[Long] =
    q.get("user_id") match {
      case None | Some("") => IO.raiseError(AppError.ExternalUserIDRequired("user_id is required"))
      case Some(v) =>
        v.toLongOption match {
          case Some(id) => IO.pure(id)
          case None     => IO.raiseError(AppError.ExternalInvalidUserID("invalid user_id"))
        }
    }

  private def intParam(q: Map[String, String], name: String, default: Int): Int =
    q.get(name).flatMap(_.toIntOption).filter(_ >= 1).getOrElse(default)

  private def listV1(userId: Long, q: Map[String, String]): IO[Response[IO]] = {
    val page     = intParam(q, "page", 1)
    val pageSize = intParam(q, "page_size", 10)
    for {
      result       <- repo.listOffsetForExternalAPI(userId, page, pageSize)
      (tasks, total) = result
      resp         <- json(Status.Ok, ExternalJson.listV1Response(tasks, page, pageSize, total))
    } yield resp
  }

  private def listV2(userId: Long, q: Map[String, String]): IO[Response[IO]] = {
    val limit = intParam(q, "limit", 10)
    for {
      after <- q.get("cursor").filter(_.nonEmpty) match {
        case None => IO.pure(None)
        case Some(c) =>
          ExternalJson.decodeCursor(c) match {
            case Some(v) => IO.pure(Some(v))
            case None    => IO.raiseError(AppError.ExternalInvalidCursor("cursorの形式が不正です"))
          }
      }
      tasks <- repo.listCursorForExternalAPI(userId, after, limit)
      nextCursor = if (tasks.length == limit) {
        val last = tasks.last
        Some(ExternalJson.encodeCursor(last.createdAt, last.id))
      } else None
      resp <- json(Status.Ok, ExternalJson.listV2Response(tasks, nextCursor, limit))
    } yield resp
  }

  val routes: HttpRoutes[IO] = HttpRoutes.of[IO] {
    case req @ GET -> Root / "external" / "v1" / "tasks" =>
      (for {
        _        <- authenticate(req)
        q         = req.uri.query.params
        userId   <- parseUserID(q)
        useV2    <- flags.paginationV2
        resp     <- if (useV2) listV2(userId, q) else listV1(userId, q)
      } yield resp).handleErrorWith(errorResponse)
  }
}
