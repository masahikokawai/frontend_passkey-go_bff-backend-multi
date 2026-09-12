package com.bffgin.backendpekko

import java.time.Instant
import scala.concurrent.{ExecutionContext, Future}

// CONTRACT.mdセクション11: 外部公開API(/external/v1/tasks)のkeysetページングが指す
// 「直前ページの最後の行の位置」。backend/internal/repository/task.go の TaskCursor に対応
final case class ExternalCursor(createdAt: Instant, id: Long)

// TaskService(業務ロジック層)がDBアクセス方式に依存しないようにするtrait
// backend/internal/repository/task.go の役割に対応。テストでは実DBを使わない
// InMemoryTaskRepository(TaskServiceSpec.scala)に差し替える
trait TaskRepository {
  // REST v1向け: offsetベースのページング。totalは絞り込み後の全件数
  // backend/internal/repository/task.go の ListWithoutLabels 相当(ただしN+1は再現しない、
  // CONTRACT.mdセクション20.4参照)
  def list(
      userId: Long,
      name: Option[String],
      status: Option[String],
      labelIds: Seq[Long],
      sort: Option[String],
      limit: Int,
      offset: Int
  )(implicit ec: ExecutionContext): Future[(Seq[TaskDTO], Int)]

  // gRPC v2向け: cursor(keyset)ベースのページング。常にid昇順固定
  // backend/internal/repository/task.go の ListWithLabelsPreloaded(UseCursor)相当
  def listCursor(
      userId: Long,
      name: Option[String],
      status: Option[String],
      labelIds: Seq[Long],
      cursor: Long,
      limit: Int
  )(implicit ec: ExecutionContext): Future[(Seq[TaskDTO], Long)]

  def get(id: Long, userId: Long)(implicit ec: ExecutionContext): Future[Option[TaskDTO]]
  def create(userId: Long, input: TaskInput)(implicit ec: ExecutionContext): Future[TaskDTO]
  def update(id: Long, userId: Long, input: TaskInput)(implicit ec: ExecutionContext): Future[Option[TaskDTO]]
  def delete(id: Long, userId: Long)(implicit ec: ExecutionContext): Future[Boolean]

  // 外部公開API v1向け(セクション11): offsetページング。並び順は created_at DESC, id DESC 固定
  // (内部CRUDのlist/listCursorとは別軸・別ソート順のため、既存メソッドは変更せず追加する)
  def listOffsetExternal(userId: Long, page: Int, pageSize: Int)(implicit
      ec: ExecutionContext
  ): Future[(Seq[TaskDTO], Int)]

  // 外部公開API v2向け: keyset(cursor)ページング。並び順は created_at DESC, id DESC 固定
  // cursorAfterがNoneなら先頭ページ。次ページが無ければ戻り値のOptionはNone
  def listCursorExternal(userId: Long, cursorAfter: Option[ExternalCursor], limit: Int)(implicit
      ec: ExecutionContext
  ): Future[Seq[TaskDTO]]
}

trait UserRepository {
  def get(id: Long)(implicit ec: ExecutionContext): Future[Option[UserDTO]]
  def getByKeycloakSub(sub: String)(implicit ec: ExecutionContext): Future[Option[UserDTO]]
}
