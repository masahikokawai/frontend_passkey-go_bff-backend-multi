package com.bffgin.backendpekko.external

import org.apache.pekko.http.scaladsl.marshallers.sprayjson.SprayJsonSupport._
import org.apache.pekko.http.scaladsl.model.StatusCodes
import org.apache.pekko.http.scaladsl.server.Directives._
import org.apache.pekko.http.scaladsl.server.{Directive1, Route}

import com.bffgin.backendpekko.JsonProtocol.{ErrorBody, errorBodyFormat}
import com.bffgin.backendpekko.ExternalJsonProtocol._
import com.bffgin.backendpekko._
import com.bffgin.backendpekko.auth.{Claims, JwtDispatcher}

import scala.concurrent.ExecutionContext

// backend/internal/handler/external/task.go(GET /external/v1/tasks)に対応
// CONTRACT.mdセクション11・20.7: BFFを経由しないサーバー間連携用API。認証はClient
// Credentials Grantで発行されたトークンのみを受け付け(azpクレームで発行先クライアントを検証)、
// ページネーション方式(offset/keyset)は5言語で共有する backend.external-tasks-pagination-v2
// フラグ(flagPollerが直近の評価結果をキャッシュしている)で切り替える
final class ExternalTaskRoutes(
    taskService: TaskService,
    dispatcher: JwtDispatcher,
    expectedClientId: String,
    flagSource: FlagSource
)(implicit ec: ExecutionContext) {

  // backend/internal/authjwt/external_middleware.go の RequireExternalClientAuth に対応
  private def authenticateExternalClient: Directive1[Claims] =
    optionalHeaderValueByName("Authorization").flatMap {
      case Some(h) if h.startsWith("Bearer ") =>
        dispatcher.verify(h.stripPrefix("Bearer ")) match {
          case Right(claims) if claims.azp.contains(expectedClientId) => provide(claims)
          case Right(_) => complete(StatusCodes.Forbidden -> ErrorBody("client_not_allowed"))
          case Left(_)  => complete(StatusCodes.Unauthorized -> ErrorBody("invalid_token"))
        }
      case _ => complete(StatusCodes.Unauthorized -> ErrorBody("unauthorized"))
    }

  val routes: Route =
    pathPrefix("external" / "v1" / "tasks") {
      pathEndOrSingleSlash {
        get {
          authenticateExternalClient { _ =>
            parameter("user_id".optional) {
              case None => complete(StatusCodes.BadRequest -> ErrorBody("user_id is required"))
              case Some(userIdStr) =>
                userIdStr.toLongOption match {
                  case None => complete(StatusCodes.BadRequest -> ErrorBody("invalid user_id"))
                  case Some(userId) =>
                    if (flagSource.currentValue) listV2(userId) else listV1(userId)
                }
            }
          }
        }
      }
    }

  private def listV1(userId: Long): Route =
    (parameter("page".as[Int].optional) & parameter("page_size".as[Int].optional)) { (pageQ, pageSizeQ) =>
      val page = pageQ.filter(_ >= 1).getOrElse(1)
      val pageSize = pageSizeQ.filter(_ >= 1).getOrElse(10)
      onSuccess(taskService.listOffsetExternal(userId, page, pageSize)) { case (dtos, total) =>
        complete(ExternalListV1Response(dtos, page, pageSize, total))
      }
    }

  private def listV2(userId: Long): Route =
    (parameter("cursor".optional) & parameter("limit".as[Int].optional)) { (cursorQ, limitQ) =>
      val limit = limitQ.filter(_ >= 1).getOrElse(10)
      val cursorAfter = cursorQ.filter(_.nonEmpty) match {
        case None => Right(None)
        case Some(c) => ExternalCursorCodec.decode(c).map(Some(_))
      }
      cursorAfter match {
        case Left(_) => complete(StatusCodes.UnprocessableEntity -> ErrorBody("invalid_cursor"))
        case Right(after) =>
          onSuccess(taskService.listCursorExternal(userId, after, limit)) { dtos =>
            val nextCursor =
              if (dtos.size == limit) dtos.lastOption.map(t => ExternalCursorCodec.encode(ExternalCursor(t.createdAt, t.id)))
              else None
            complete(ExternalListV2Response(dtos, nextCursor, limit))
          }
      }
    }
}
