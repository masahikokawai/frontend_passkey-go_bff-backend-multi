package com.bffgin.backendpekko.rest

import org.apache.pekko.http.scaladsl.marshallers.sprayjson.SprayJsonSupport._
import org.apache.pekko.http.scaladsl.model.StatusCodes
import org.apache.pekko.http.scaladsl.server.Directives._
import org.apache.pekko.http.scaladsl.server.{Directive1, Route}

import com.bffgin.backendpekko.JsonProtocol._
import com.bffgin.backendpekko._
import com.bffgin.backendpekko.auth.{Claims, JwtDispatcher}

import scala.concurrent.ExecutionContext

// backend/internal/handler/v1/task.go(REST v1)に対応するPekko HTTPルート
// CONTRACT.mdセクション20.5(ワイヤー契約パリティ)の通り、JSON形状・ステータスコード・
// エラーボディをGo実装と1対1で一致させる
final class TaskRoutes(taskService: TaskService, dispatcher: JwtDispatcher)(implicit ec: ExecutionContext) {

  private def authenticate: Directive1[Claims] =
    optionalHeaderValueByName("Authorization").flatMap {
      case Some(h) if h.startsWith("Bearer ") =>
        dispatcher.verify(h.stripPrefix("Bearer ")) match {
          case Right(claims) => provide(claims)
          case Left(_)        => complete(StatusCodes.Unauthorized -> ErrorBody("invalid_token"))
        }
      case _ => complete(StatusCodes.Unauthorized -> ErrorBody("unauthorized"))
    }

  // authenticate + resolveUserId をまとめたディレクティブ。Go実装のresolveUserIDに対応
  private def authenticatedUserId: Directive1[Long] =
    authenticate.flatMap { claims =>
      onSuccess(taskService.resolveUserId(claims)).flatMap {
        case Right(id)                              => provide(id)
        case Left(ServiceError.UserNotProvisioned)  => complete(StatusCodes.Forbidden -> ErrorBody("user_not_provisioned"))
        case Left(_)                                 => complete(StatusCodes.InternalServerError -> ErrorBody("internal_server_error"))
      }
    }

  private def parsedBody: Directive1[TaskInput] =
    entity(as[TaskRequestBody]).flatMap { body =>
      parseTaskInput(body) match {
        case Right(input)                          => provide(input)
        case Left(ParseError.InvalidRequest)       => complete(StatusCodes.BadRequest -> ErrorBody("invalid_request"))
        case Left(ParseError.InvalidFinishedOn)    => complete(StatusCodes.UnprocessableEntity -> ErrorBody("invalid_finished_on"))
      }
    }

  private def renderError(err: ServiceError): Route = err match {
    case ServiceError.NotFound             => complete(StatusCodes.NotFound -> ErrorBody("not_found"))
    case ServiceError.Validation(msg)      => complete(StatusCodes.UnprocessableEntity -> ErrorBody("validation_error", Some(msg)))
    case ServiceError.UserNotProvisioned   => complete(StatusCodes.Forbidden -> ErrorBody("user_not_provisioned"))
  }

  val routes: Route =
    pathPrefix("internal" / "v1" / "tasks") {
      authenticatedUserId { userId =>
        concat(
          pathEndOrSingleSlash {
            concat(
              get {
                (parameter("name".optional) & parameter("status".optional) & parameter("label_ids".optional) &
                  parameter("sort".optional) & parameter("limit".as[Int].optional) & parameter("offset".as[Int].optional)) {
                  (nameQ, statusQ, labelIdsQ, sortQ, limitQ, offsetQ) =>
                    statusQ match {
                      case Some(s) if !TaskStatus.isValid(s) =>
                        complete(StatusCodes.UnprocessableEntity -> ErrorBody("invalid_status"))
                      case _ =>
                        val labelIds: Seq[Long] =
                          labelIdsQ.map(_.split(",").toIndexedSeq.flatMap(_.toLongOption)).getOrElse(Seq.empty)
                        val limit = limitQ.getOrElse(20)
                        val offset = offsetQ.getOrElse(0)
                        onSuccess(taskService.list(userId, nameQ, statusQ, labelIds, sortQ, limit, offset)) {
                          case (dtos, total) => complete(TaskListResponse(dtos, total, limit, offset))
                        }
                    }
                }
              },
              post {
                parsedBody { input =>
                  onSuccess(taskService.create(userId, input)) {
                    case Right(dto) => complete(StatusCodes.Created -> dto)
                    case Left(err)  => renderError(err)
                  }
                }
              }
            )
          },
          path(LongNumber) { id =>
            concat(
              get {
                onSuccess(taskService.get(id, userId)) {
                  case Right(dto) => complete(dto)
                  case Left(err)  => renderError(err)
                }
              },
              patch {
                parsedBody { input =>
                  onSuccess(taskService.update(id, userId, input)) {
                    case Right(dto) => complete(dto)
                    case Left(err)  => renderError(err)
                  }
                }
              },
              delete {
                onSuccess(taskService.delete(id, userId)) {
                  case Right(_)  => complete(StatusCodes.NoContent)
                  case Left(err) => renderError(err)
                }
              }
            )
          }
        )
      }
    }
}
