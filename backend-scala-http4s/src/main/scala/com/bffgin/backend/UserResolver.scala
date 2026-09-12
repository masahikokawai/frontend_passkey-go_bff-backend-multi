package com.bffgin.backend

import cats.effect.IO
import com.bffgin.backend.auth.{Claims, JwtAuth}

/** backend/internal/handler/v1/task.go・grpcserver/task_service.go の resolveUserID を再現する
  * ローカル発行issの場合はsubが内部user_idそのもの、Keycloak発行issの場合はsubがkeycloak_sub
  * どちらも見つからなければAppError.UserNotProvisionedを投げる(REST 403 / gRPC PermissionDenied)
  */
object UserResolver {
  def resolve(claims: Claims, repo: UserLookup, auth: JwtAuth): IO[Long] =
    if (auth.isLocalIssuer(claims.issuer)) {
      claims.subject.toLongOption match {
        case None => IO.raiseError(AppError.UserNotProvisioned)
        case Some(id) =>
          repo.findUserById(id).flatMap {
            case Some(u) => IO.pure(u.id)
            case None    => IO.raiseError(AppError.UserNotProvisioned)
          }
      }
    } else {
      repo.findUserByKeycloakSub(claims.subject).flatMap {
        case Some(u) => IO.pure(u.id)
        case None    => IO.raiseError(AppError.UserNotProvisioned)
      }
    }
}
