package com.bffgin.backendpekko

import com.bffgin.backendpekko.auth.{Claims, JwtIssuers}

import java.time.{LocalDate, ZoneOffset}
import scala.concurrent.{ExecutionContext, Future}

// backend/internal/service/task.go の TaskService に対応
// List系のstatusクエリパラメータのバリデーション(invalid_status)は、Goのhandler層と同じく
// ここではなくrest/TaskRoutes.scala・grpc/TaskGrpcServiceImpl.scala側で行う
// (Create/Updateのvalidation_errorとは異なるエラー種別のため、意図的に分離している)
final class TaskService(taskRepo: TaskRepository, userRepo: UserRepository) {

  // backend/internal/handler/v1/task.go・grpcserver/task_service.go の resolveUserID に対応
  // ローカル発行issの場合はsubが内部user_idそのもの、Keycloak発行issの場合はsubがkeycloak_sub
  def resolveUserId(claims: Claims)(implicit ec: ExecutionContext): Future[Either[ServiceError, Long]] =
    if (JwtIssuers.isLocal(claims.issuer)) {
      claims.subject.toLongOption match {
        case None => Future.successful(Left(ServiceError.UserNotProvisioned))
        case Some(id) =>
          userRepo.get(id).map {
            case Some(u) => Right(u.id)
            case None    => Left(ServiceError.UserNotProvisioned)
          }
      }
    } else {
      userRepo.getByKeycloakSub(claims.subject).map {
        case Some(u) => Right(u.id)
        case None    => Left(ServiceError.UserNotProvisioned)
      }
    }

  // backend/internal/service/task.go の validateTaskInput に対応
  //   - name: 必須・20文字以内(コードポイント単位、マルチバイト対応)
  //   - finished_on: 過去日不可
  //   - status: enumの範囲内
  // 【テスト監査で発見・修正した実バグ】todayパラメータは以前存在せず、内部で直接
  // LocalDate.now()(JVMのデフォルトタイムゾーン=実行環境のOS設定次第で不定)を呼んでいた。
  // finished_on自体はタイムゾーンを持たない素の日付として比較されるため、
  // 比較対象の「今日」だけがタイムゾーン依存になっているのは非対称。この不定さは
  // Rust/Rails(いずれも明示的にUTC基準)との間で、同一リクエスト・同一時刻に対して
  // 受理/拒否の判定が割れる契約違反(CONTRACT.mdセクション20.5)を引き起こしうる
  // (例: 実行環境がJST(UTC+9)の場合、UTC 15:00〜23:59の間はこの実装だけ「今日」の判定が
  // 他言語より1日進んでしまう)。UTC固定のデフォルト引数にすることでRust/Railsと同じ基準に揃え、
  // かつテストからは任意の日付を注入できるようにする(既存呼び出し元は引数追加不要)
  def validate(input: TaskInput, today: LocalDate = LocalDate.now(ZoneOffset.UTC)): Either[ServiceError.Validation, Unit] = {
    if (input.name.isEmpty) Left(ServiceError.Validation("nameは必須です"))
    else if (input.name.codePointCount(0, input.name.length) > 20)
      Left(ServiceError.Validation("nameは20文字以内である必要があります"))
    else if (input.finishedOn.isBefore(today))
      Left(ServiceError.Validation("finished_onに過去日は指定できません"))
    else if (!TaskStatus.isValid(input.status))
      Left(ServiceError.Validation(s"不明なstatus: ${input.status}"))
    else Right(())
  }

  def list(
      userId: Long,
      name: Option[String],
      status: Option[String],
      labelIds: Seq[Long],
      sort: Option[String],
      limit: Int,
      offset: Int
  )(implicit ec: ExecutionContext): Future[(Seq[TaskDTO], Int)] =
    taskRepo.list(userId, name, status, labelIds, sort, limit, offset)

  def listCursor(
      userId: Long,
      name: Option[String],
      status: Option[String],
      labelIds: Seq[Long],
      cursor: Long,
      limit: Int
  )(implicit ec: ExecutionContext): Future[(Seq[TaskDTO], Long)] =
    taskRepo.listCursor(userId, name, status, labelIds, cursor, limit)

  // CONTRACT.mdセクション11: 外部公開API向け。内部CRUDのlist/listCursorとはソート順・
  // 対象範囲が異なるため、taskRepoの専用メソッドへそのまま委譲するだけの薄いラッパー
  def listOffsetExternal(userId: Long, page: Int, pageSize: Int)(implicit
      ec: ExecutionContext
  ): Future[(Seq[TaskDTO], Int)] =
    taskRepo.listOffsetExternal(userId, page, pageSize)

  def listCursorExternal(userId: Long, cursorAfter: Option[ExternalCursor], limit: Int)(implicit
      ec: ExecutionContext
  ): Future[Seq[TaskDTO]] =
    taskRepo.listCursorExternal(userId, cursorAfter, limit)

  def get(id: Long, userId: Long)(implicit ec: ExecutionContext): Future[Either[ServiceError, TaskDTO]] =
    taskRepo.get(id, userId).map {
      case Some(dto) => Right(dto)
      case None      => Left(ServiceError.NotFound)
    }

  def create(userId: Long, input: TaskInput)(implicit ec: ExecutionContext): Future[Either[ServiceError, TaskDTO]] =
    validate(input) match {
      case Left(e)  => Future.successful(Left(e))
      case Right(_) => taskRepo.create(userId, input).map(Right(_))
    }

  def update(id: Long, userId: Long, input: TaskInput)(implicit ec: ExecutionContext): Future[Either[ServiceError, TaskDTO]] =
    validate(input) match {
      case Left(e) => Future.successful(Left(e))
      case Right(_) =>
        taskRepo.update(id, userId, input).map {
          case Some(dto) => Right(dto)
          case None      => Left(ServiceError.NotFound)
        }
    }

  def delete(id: Long, userId: Long)(implicit ec: ExecutionContext): Future[Either[ServiceError, Unit]] =
    taskRepo.delete(id, userId).map(ok => if (ok) Right(()) else Left(ServiceError.NotFound))
}
