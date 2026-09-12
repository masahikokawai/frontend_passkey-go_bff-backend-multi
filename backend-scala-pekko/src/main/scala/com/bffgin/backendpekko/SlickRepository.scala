package com.bffgin.backendpekko

import slick.jdbc.MySQLProfile.api._
import Tables._

import java.sql.{Date => SqlDate, Timestamp}
import java.time.Instant
import scala.concurrent.{ExecutionContext, Future}

// backend/internal/repository/task.go の Task に対応。scoped/applySortの絞り込みロジックは
// GoRMのそれと1対1で対応させている(name LIKE、status完全一致、label_idsはEXISTSサブクエリ、
// sortはfinished_on asc/desc、それ以外はcreated_at desc)
final class SlickTaskRepository(db: Database) extends TaskRepository {

  private def baseQuery(
      userId: Long,
      name: Option[String],
      status: Option[String],
      labelIds: Seq[Long]
  ): Query[TasksTable, TaskRow, Seq] = {
    var q: Query[TasksTable, TaskRow, Seq] = tasks.filter(_.userId === userId)
    name.filter(_.nonEmpty).foreach { n =>
      q = q.filter(_.name like s"%$n%")
    }
    status.flatMap(TaskStatus.toInt).foreach { s =>
      q = q.filter(_.status === s)
    }
    if (labelIds.nonEmpty) {
      q = q.filter(t => taskLabels.filter(tl => tl.taskId === t.id && tl.labelId.inSet(labelIds)).exists)
    }
    q
  }

  private def attachLabels(rows: Seq[TaskRow])(implicit ec: ExecutionContext): Future[Seq[TaskDTO]] =
    if (rows.isEmpty) Future.successful(Seq.empty)
    else {
      val ids = rows.map(_.id)
      val labelQuery = taskLabels
        .filter(_.taskId.inSet(ids))
        .join(labels)
        .on(_.labelId === _.id)
        .map { case (tl, l) => (tl.taskId, l.id, l.name) }
      db.run(labelQuery.result).map { pairs =>
        val grouped = pairs
          .groupBy(_._1)
          .view
          .mapValues(_.map { case (_, id, nm) => LabelDTO(id, nm) })
          .toMap
        rows.map(r => toDTO(r, grouped.getOrElse(r.id, Seq.empty)))
      }
    }

  private def toDTO(r: TaskRow, labelSeq: Seq[LabelDTO]): TaskDTO =
    TaskDTO(
      id = r.id,
      name = r.name,
      description = r.description,
      status = TaskStatus.fromInt(r.status),
      finishedOn = r.finishedOn.toLocalDate,
      userId = r.userId,
      labels = labelSeq,
      createdAt = r.createdAt.toInstant,
      updatedAt = r.updatedAt.toInstant
    )

  override def list(
      userId: Long,
      name: Option[String],
      status: Option[String],
      labelIds: Seq[Long],
      sort: Option[String],
      limit: Int,
      offset: Int
  )(implicit ec: ExecutionContext): Future[(Seq[TaskDTO], Int)] = {
    val filtered = baseQuery(userId, name, status, labelIds)
    val sorted = sort match {
      case Some("asc")  => filtered.sortBy(_.finishedOn.asc)
      case Some("desc") => filtered.sortBy(_.finishedOn.desc)
      case _            => filtered.sortBy(_.createdAt.desc)
    }
    for {
      total <- db.run(filtered.length.result)
      rows <- db.run(sorted.drop(offset).take(limit).result)
      dtos <- attachLabels(rows)
    } yield (dtos, total)
  }

  override def listCursor(
      userId: Long,
      name: Option[String],
      status: Option[String],
      labelIds: Seq[Long],
      cursor: Long,
      limit: Int
  )(implicit ec: ExecutionContext): Future[(Seq[TaskDTO], Long)] = {
    val filteredBase = baseQuery(userId, name, status, labelIds)
    val filtered = if (cursor > 0) filteredBase.filter(_.id > cursor) else filteredBase
    val sorted = filtered.sortBy(_.id.asc).take(limit + 1)
    for {
      rows <- db.run(sorted.result)
      page = rows.take(limit)
      dtos <- attachLabels(page)
    } yield {
      val nextCursor = if (rows.length > limit) page.lastOption.map(_.id).getOrElse(0L) else 0L
      (dtos, nextCursor)
    }
  }

  override def get(id: Long, userId: Long)(implicit ec: ExecutionContext): Future[Option[TaskDTO]] =
    db.run(tasks.filter(t => t.id === id && t.userId === userId).result.headOption).flatMap {
      case None      => Future.successful(None)
      case Some(row) => attachLabels(Seq(row)).map(_.headOption)
    }

  private def replaceLabels(taskId: Long, labelIds: Seq[Long], now: Timestamp): DBIO[Unit] = {
    val deleteAction = taskLabels.filter(_.taskId === taskId).delete
    val insertRows = labelIds.distinct.map(lid => TaskLabelRow(0L, taskId, lid, now, now))
    val insertAction = taskLabels ++= insertRows
    DBIO.seq(deleteAction, insertAction)
  }

  override def create(userId: Long, input: TaskInput)(implicit ec: ExecutionContext): Future[TaskDTO] = {
    val now = Timestamp.from(Instant.now())
    val statusInt = TaskStatus.toInt(input.status).getOrElse(1)
    val insertRow =
      TaskRow(0L, input.name, input.description, statusInt, SqlDate.valueOf(input.finishedOn), userId, now, now)
    val action = (tasks returning tasks.map(_.id)) += insertRow
    for {
      newId <- db.run(action)
      _ <- db.run(replaceLabels(newId, input.labelIds, now).transactionally)
      dto <- get(newId, userId).map(_.getOrElse(throw new IllegalStateException("作成直後のtaskが見つからない")))
    } yield dto
  }

  override def update(id: Long, userId: Long, input: TaskInput)(implicit ec: ExecutionContext): Future[Option[TaskDTO]] = {
    val now = Timestamp.from(Instant.now())
    val statusInt = TaskStatus.toInt(input.status).getOrElse(1)
    val updateAction = tasks
      .filter(t => t.id === id && t.userId === userId)
      .map(t => (t.name, t.description, t.status, t.finishedOn, t.updatedAt))
      .update((input.name, input.description, statusInt, SqlDate.valueOf(input.finishedOn), now))
    for {
      affected <- db.run(updateAction)
      result <-
        if (affected == 0) Future.successful(None)
        else db.run(replaceLabels(id, input.labelIds, now).transactionally).flatMap(_ => get(id, userId))
    } yield result
  }

  // 【3回目のIDOR監査で発見・修正、backend-scala-http4sの同種バグと同じ原因】
  // 以前はtaskLabelsを所有者チェックより先に無条件削除しており、他人のtask idを
  // 指定してdeleteを呼ぶだけで、タスク本体はuserId条件で守られるものの、
  // ラベル関連付けだけは誰でも消せてしまう脆弱性があった。updateメソッド(上記)は
  // affected == 0 でガードしているのに、deleteだけ抜けていた非対称。
  // tasks側のdeleteを先に実行し、実際にそのユーザーが所有する行が消えた場合のみ
  // taskLabelsも削除するよう順序を入れ替える
  override def delete(id: Long, userId: Long)(implicit ec: ExecutionContext): Future[Boolean] = {
    val action = for {
      affected <- tasks.filter(t => t.id === id && t.userId === userId).delete
      _ <- if (affected > 0) taskLabels.filter(_.taskId === id).delete else DBIO.successful(0)
    } yield affected
    db.run(action.transactionally).map(_ > 0)
  }

  // CONTRACT.mdセクション11: 外部公開API v1(offsetページング)
  // backend/internal/repository/task.go の ListOffsetForExternalAPI に対応
  // 「created_at DESC, id DESC」で安定ソートする(内部CRUDのlistとはソート・対象が異なるため
  // 既存メソッドは変更せず、この専用メソッドを追加する)
  override def listOffsetExternal(userId: Long, page: Int, pageSize: Int)(implicit
      ec: ExecutionContext
  ): Future[(Seq[TaskDTO], Int)] = {
    val filtered = tasks.filter(_.userId === userId)
    val sorted = filtered.sortBy(t => (t.createdAt.desc, t.id.desc))
    val offset = (page - 1) * pageSize
    for {
      total <- db.run(filtered.length.result)
      rows <- db.run(sorted.drop(offset).take(pageSize).result)
      dtos <- attachLabels(rows)
    } yield (dtos, total)
  }

  // CONTRACT.mdセクション11: 外部公開API v2(keyset/cursorページング)
  // backend/internal/repository/task.go の ListCursorForExternalAPI に対応
  // (created_at, id)の複合条件でOFFSETを使わずに絞り込む(keysetページングの定石)
  override def listCursorExternal(userId: Long, cursorAfter: Option[ExternalCursor], limit: Int)(implicit
      ec: ExecutionContext
  ): Future[Seq[TaskDTO]] = {
    val base = tasks.filter(_.userId === userId)
    val filtered = cursorAfter match {
      case None => base
      case Some(c) =>
        val ts = Timestamp.from(c.createdAt)
        base.filter(t => (t.createdAt < ts) || (t.createdAt === ts && t.id < c.id))
    }
    val sorted = filtered.sortBy(t => (t.createdAt.desc, t.id.desc)).take(limit)
    for {
      rows <- db.run(sorted.result)
      dtos <- attachLabels(rows)
    } yield dtos
  }
}

// users.keycloak_subは既に存在しない(000008で分離済み、Db.scala参照)ため、
// keycloak_subでの検索はuser_keycloaksをusersへjoinして行う
final class SlickUserRepository(db: Database) extends UserRepository {
  override def get(id: Long)(implicit ec: ExecutionContext): Future[Option[UserDTO]] =
    db.run(users.filter(_.id === id).result.headOption)
      .map(_.map(r => UserDTO(r.id, keycloakSub = "", r.email, r.name, r.role)))

  override def getByKeycloakSub(sub: String)(implicit ec: ExecutionContext): Future[Option[UserDTO]] = {
    val q = userKeycloaks
      .filter(_.keycloakSub === sub)
      .join(users)
      .on(_.userId === _.id)
      .map { case (_, u) => u }
    db.run(q.result.headOption).map(_.map(r => UserDTO(r.id, sub, r.email, r.name, r.role)))
  }
}
