package com.bffgin.backendpekko

import java.time.Instant
import java.util.concurrent.atomic.AtomicLong
import scala.collection.mutable
import scala.concurrent.{ExecutionContext, Future}

// 実DBを使わないTaskServiceSpec専用のテストダブル
// backend側のテストがGORMをモックせず素朴なfakeを使うのと同じ考え方
class InMemoryTaskRepository extends TaskRepository {
  private val store = mutable.Map.empty[Long, TaskDTO]
  private val nextId = new AtomicLong(1)

  def seed(dto: TaskDTO): Unit = store.update(dto.id, dto)

  override def list(
      userId: Long,
      name: Option[String],
      status: Option[String],
      labelIds: Seq[Long],
      sort: Option[String],
      limit: Int,
      offset: Int
  )(implicit ec: ExecutionContext): Future[(Seq[TaskDTO], Int)] = {
    val filtered = store.values.filter(_.userId == userId).toSeq
    Future.successful((filtered.slice(offset, offset + limit), filtered.size))
  }

  override def listCursor(
      userId: Long,
      name: Option[String],
      status: Option[String],
      labelIds: Seq[Long],
      cursor: Long,
      limit: Int
  )(implicit ec: ExecutionContext): Future[(Seq[TaskDTO], Long)] = {
    val filtered = store.values.filter(t => t.userId == userId && t.id > cursor).toSeq.sortBy(_.id)
    val page = filtered.take(limit)
    val next = if (filtered.size > limit) page.lastOption.map(_.id).getOrElse(0L) else 0L
    Future.successful((page, next))
  }

  override def get(id: Long, userId: Long)(implicit ec: ExecutionContext): Future[Option[TaskDTO]] =
    Future.successful(store.get(id).filter(_.userId == userId))

  override def create(userId: Long, input: TaskInput)(implicit ec: ExecutionContext): Future[TaskDTO] = {
    val id = nextId.getAndIncrement()
    val now = Instant.now()
    val dto = TaskDTO(
      id,
      input.name,
      input.description,
      input.status,
      input.finishedOn,
      userId,
      input.labelIds.map(lid => LabelDTO(lid, s"label-$lid")),
      now,
      now
    )
    store.update(id, dto)
    Future.successful(dto)
  }

  override def update(id: Long, userId: Long, input: TaskInput)(implicit ec: ExecutionContext): Future[Option[TaskDTO]] =
    store.get(id).filter(_.userId == userId) match {
      case None => Future.successful(None)
      case Some(existing) =>
        val updated = existing.copy(
          name = input.name,
          description = input.description,
          status = input.status,
          finishedOn = input.finishedOn,
          labels = input.labelIds.map(lid => LabelDTO(lid, s"label-$lid")),
          updatedAt = Instant.now()
        )
        store.update(id, updated)
        Future.successful(Some(updated))
    }

  override def delete(id: Long, userId: Long)(implicit ec: ExecutionContext): Future[Boolean] =
    store.get(id).filter(_.userId == userId) match {
      case None => Future.successful(false)
      case Some(_) =>
        store.remove(id)
        Future.successful(true)
    }

  // CONTRACT.mdセクション11: 外部公開API向け。created_at DESC, id DESC固定ソート
  private def externalSorted(userId: Long): Seq[TaskDTO] =
    store.values.filter(_.userId == userId).toSeq.sortBy(t => (t.createdAt.toEpochMilli, t.id))(Ordering.Tuple2(Ordering.Long.reverse, Ordering.Long.reverse))

  override def listOffsetExternal(userId: Long, page: Int, pageSize: Int)(implicit
      ec: ExecutionContext
  ): Future[(Seq[TaskDTO], Int)] = {
    val all = externalSorted(userId)
    val offset = (page - 1) * pageSize
    Future.successful((all.slice(offset, offset + pageSize), all.size))
  }

  override def listCursorExternal(userId: Long, cursorAfter: Option[ExternalCursor], limit: Int)(implicit
      ec: ExecutionContext
  ): Future[Seq[TaskDTO]] = {
    val all = externalSorted(userId)
    val filtered = cursorAfter match {
      case None => all
      case Some(c) =>
        all.filter(t => t.createdAt.isBefore(c.createdAt) || (t.createdAt == c.createdAt && t.id < c.id))
    }
    Future.successful(filtered.take(limit))
  }
}

class InMemoryUserRepository extends UserRepository {
  private val byId = mutable.Map.empty[Long, UserDTO]
  private val byKeycloakSub = mutable.Map.empty[String, UserDTO]

  def seed(u: UserDTO): Unit = {
    byId.update(u.id, u)
    if (u.keycloakSub.nonEmpty) byKeycloakSub.update(u.keycloakSub, u)
  }

  override def get(id: Long)(implicit ec: ExecutionContext): Future[Option[UserDTO]] =
    Future.successful(byId.get(id))

  override def getByKeycloakSub(sub: String)(implicit ec: ExecutionContext): Future[Option[UserDTO]] =
    Future.successful(byKeycloakSub.get(sub))
}
