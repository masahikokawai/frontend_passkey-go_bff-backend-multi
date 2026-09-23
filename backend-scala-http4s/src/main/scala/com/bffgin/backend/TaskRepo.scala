package com.bffgin.backend

import cats.effect.IO
import cats.syntax.all._
import doobie._
import doobie.implicits._
import doobie.hikari.HikariTransactor
import java.time.{LocalDate, LocalDateTime}
import doobie.implicits.javatimedrivernative._

final case class TaskListFilter(
    userId: Long,
    name: String,
    status: Option[TaskStatus],
    labelIds: List[Long],
    limit: Int,
    offset: Int
)

final case class TaskCursorFilter(
    userId: Long,
    name: String,
    status: Option[TaskStatus],
    labelIds: List[Long],
    cursor: Long,
    limit: Int
)

// tasks/labels/task_labels/users は backend/migrations(golang-migrate)が正本のスキーマ
// (CONTRACT.mdセクション20.5)。ここでは既存テーブルへ接続するだけで、独自マイグレーションは持たない
//
// 【IOモナドについて、backend-haskellとの対比】このファイルの全メソッドが返す`cats.effect.IO[...]`は、
// 「副作用を伴う計算を、実行せずに値として組み立て、呼び出し側(main等)が最終的に一度だけ
// 実行する」という設計を型で強制する。backend-haskell(src/BackendHaskell/Repository/TaskRepository.hs)
// もほぼ同じ発想で、全公開関数が`IO`アクションを返す。異なるのは、Haskellの`IO`は言語のRTSに
// 組み込まれたプリミティブ型(すべての副作用がこれを経由する唯一の道)であるのに対し、
// cats-effectの`IO`はサードパーティのライブラリが提供するデータ型であり、Scala言語自体は
// 副作用の分離を強制しない(通常のメソッドで直接副作用を起こすことも可能)という点。
// 詳細はbackend-haskell/README.md「IOモナドについて」節を参照
class TaskRepo(xa: HikariTransactor[IO]) extends UserLookup {

  private def labelsFilterFragment(labelIds: List[Long]): Fragment =
    if (labelIds.isEmpty) Fragment.empty
    else
      fr"AND t.id IN (SELECT task_id FROM task_labels WHERE" ++
        Fragments.in(fr"label_id", cats.data.NonEmptyList.fromListUnsafe(labelIds)) ++ fr")"

  private def nameFilterFragment(name: String): Fragment =
    if (name.isEmpty) Fragment.empty else fr"AND t.name LIKE ${"%" + name + "%"}"

  private def statusFilterFragment(status: Option[TaskStatus]): Fragment =
    status.map(s => fr"AND t.status = ${s.code}").getOrElse(Fragment.empty)

  private case class TaskRow(
      id: Long,
      name: String,
      description: Option[String],
      statusCode: Int,
      finishedOn: LocalDate,
      userId: Long,
      createdAt: LocalDateTime,
      updatedAt: LocalDateTime
  )

  private def toTask(row: TaskRow, labels: List[Label]): Task =
    Task(
      id = row.id,
      name = row.name,
      description = row.description,
      status = TaskStatus.fromCode(row.statusCode).getOrElse(TaskStatus.Waiting),
      finishedOn = row.finishedOn,
      userId = row.userId,
      labels = labels,
      createdAt = row.createdAt,
      updatedAt = row.updatedAt
    )

  private def labelsForTask(taskId: Long): ConnectionIO[List[Label]] =
    sql"""SELECT l.id, l.name FROM labels l
          JOIN task_labels tl ON tl.label_id = l.id
          WHERE tl.task_id = $taskId
          ORDER BY l.id""".query[Label].to[List]

  def listOffset(filter: TaskListFilter): IO[(List[Task], Long)] = {
    val where = fr"WHERE t.user_id = ${filter.userId}" ++
      nameFilterFragment(filter.name) ++ statusFilterFragment(filter.status) ++
      labelsFilterFragment(filter.labelIds)

    val countQ = (fr"SELECT COUNT(*) FROM tasks t" ++ where).query[Long].unique
    val listQ = (fr"""SELECT t.id, t.name, t.description, t.status, t.finished_on, t.user_id, t.created_at, t.updated_at
                       FROM tasks t""" ++ where ++
      fr"ORDER BY t.created_at DESC LIMIT ${filter.limit} OFFSET ${filter.offset}")
      .query[TaskRow]
      .to[List]

    val action = for {
      total <- countQ
      rows  <- listQ
      tasks <- rows.traverse(r => labelsForTask(r.id).map(toTask(r, _)))
    } yield (tasks, total)

    action.transact(xa)
  }

  def listCursor(filter: TaskCursorFilter): IO[List[Task]] = {
    val where = fr"WHERE t.user_id = ${filter.userId}" ++
      nameFilterFragment(filter.name) ++ statusFilterFragment(filter.status) ++
      labelsFilterFragment(filter.labelIds) ++
      (if (filter.cursor > 0) fr"AND t.id > ${filter.cursor}" else Fragment.empty)

    val listQ = (fr"""SELECT t.id, t.name, t.description, t.status, t.finished_on, t.user_id, t.created_at, t.updated_at
                       FROM tasks t""" ++ where ++
      fr"ORDER BY t.id ASC LIMIT ${filter.limit}")
      .query[TaskRow]
      .to[List]

    val action = for {
      rows  <- listQ
      tasks <- rows.traverse(r => labelsForTask(r.id).map(toTask(r, _)))
    } yield tasks

    action.transact(xa)
  }

  def get(id: Long, userId: Long): IO[Option[Task]] = {
    val action = for {
      rowOpt <- sql"""SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
                       FROM tasks WHERE id = $id AND user_id = $userId"""
        .query[TaskRow]
        .option
      result <- rowOpt.traverse(r => labelsForTask(r.id).map(toTask(r, _)))
    } yield result
    action.transact(xa)
  }

  def create(userId: Long, in: TaskInput, status: TaskStatus, finishedOn: LocalDate): IO[Long] = {
    val action = for {
      id <- sql"""INSERT INTO tasks (name, description, status, finished_on, user_id, created_at, updated_at)
                   VALUES (${in.name}, ${in.description}, ${status.code}, $finishedOn, $userId, NOW(), NOW())""".update
        .withUniqueGeneratedKeys[Long]("id")
      // 【テスト監査で発見・修正した実バグ】labelIdsに同じidが重複して含まれる場合
      // (例: [3,3,5])、重複除去せずそのままINSERTすると2回目の(task_id,3)で
      // task_labelsの(task_id,label_id)へのUNIQUE制約(migrations/000004)に違反し、
      // 生のMySQLエラー(Duplicate entry)がそのまま伝播してしまう(Go実装の
      // backend/internal/repository/task.goで見つかった同種のバグと同じ根本原因)
      _ <- in.labelIds.distinct.traverse(labelId =>
        sql"""INSERT INTO task_labels (task_id, label_id, created_at, updated_at)
              VALUES ($id, $labelId, NOW(), NOW())""".update.run
      )
    } yield id
    action.transact(xa)
  }

  def update(id: Long, userId: Long, in: TaskInput, status: TaskStatus, finishedOn: LocalDate): IO[Boolean] = {
    val action = for {
      affected <- sql"""UPDATE tasks SET name = ${in.name}, description = ${in.description},
                         status = ${status.code}, finished_on = $finishedOn, updated_at = NOW()
                         WHERE id = $id AND user_id = $userId""".update.run
      _ <- if (affected > 0) {
        sql"DELETE FROM task_labels WHERE task_id = $id".update.run >>
          in.labelIds.distinct.traverse(labelId =>
            sql"""INSERT INTO task_labels (task_id, label_id, created_at, updated_at)
                  VALUES ($id, $labelId, NOW(), NOW())""".update.run
          )
      } else List.empty[Int].pure[ConnectionIO]
    } yield affected > 0
    action.transact(xa)
  }

  // 【3回目のIDOR監査で発見・修正】以前はtask_labelsを所有者チェックより先に
  // 無条件削除しており、他人のtask idを指定してDELETEを呼ぶだけで、そのタスクの
  // レコード自体は user_id 条件で守られるものの、ラベル関連付けだけは誰でも
  // 消せてしまう脆弱性があった(updateメソッドは affected > 0 でガードしているのに
  // deleteだけ抜けていた、同一ファイル内での非対称)。tasks側のDELETEを先に実行し、
  // 実際にそのユーザーが所有する行が消えた場合のみtask_labelsも削除するよう順序を入れ替える
  def delete(id: Long, userId: Long): IO[Boolean] = {
    val action = for {
      affected <- sql"DELETE FROM tasks WHERE id = $id AND user_id = $userId".update.run
      _        <- if (affected > 0) sql"DELETE FROM task_labels WHERE task_id = $id".update.run
                  else 0.pure[ConnectionIO]
    } yield affected > 0
    action.transact(xa)
  }

  // CONTRACT.mdセクション11・backend/internal/repository/task.go の
  // ListOffsetForExternalAPI をそのまま再現する。内部CRUD用のlistOffsetとは異なり
  // name/status/label_idsの絞り込みは無く、ソートも created_at DESC, id DESC で固定
  // (tie-breakにidを使うのがGoの実装との重要な違い。listOffsetはcreated_at DESCのみだった)
  def listOffsetForExternalAPI(userId: Long, page: Int, pageSize: Int): IO[(List[Task], Long)] = {
    val where = fr"WHERE t.user_id = $userId"
    val offset = (page - 1) * pageSize
    val countQ = (fr"SELECT COUNT(*) FROM tasks t" ++ where).query[Long].unique
    val listQ = (fr"""SELECT t.id, t.name, t.description, t.status, t.finished_on, t.user_id, t.created_at, t.updated_at
                       FROM tasks t""" ++ where ++
      fr"ORDER BY t.created_at DESC, t.id DESC LIMIT $pageSize OFFSET $offset")
      .query[TaskRow]
      .to[List]
    val action = for {
      total <- countQ
      rows  <- listQ
      tasks <- rows.traverse(r => labelsForTask(r.id).map(toTask(r, _)))
    } yield (tasks, total)
    action.transact(xa)
  }

  // backend/internal/repository/task.go の ListCursorForExternalAPI をそのまま再現する
  // (created_at, id)の複合条件でOFFSETを使わずに絞り込むkeysetページング
  def listCursorForExternalAPI(userId: Long, after: Option[(LocalDateTime, Long)], limit: Int): IO[List[Task]] = {
    val base = fr"WHERE t.user_id = $userId"
    val where = after match {
      case Some((createdAt, id)) =>
        base ++ fr"AND ((t.created_at < $createdAt) OR (t.created_at = $createdAt AND t.id < $id))"
      case None => base
    }
    val listQ = (fr"""SELECT t.id, t.name, t.description, t.status, t.finished_on, t.user_id, t.created_at, t.updated_at
                       FROM tasks t""" ++ where ++
      fr"ORDER BY t.created_at DESC, t.id DESC LIMIT $limit")
      .query[TaskRow]
      .to[List]
    val action = for {
      rows  <- listQ
      tasks <- rows.traverse(r => labelsForTask(r.id).map(toTask(r, _)))
    } yield tasks
    action.transact(xa)
  }

  // CONTRACT.mdセクション20.2: backend.external-tasks-pagination-v2 は5言語で共有する1つのflag。
  // backend/internal/featureflag/mysql_retriever.go の BuildFlagConfigJSON と同じ解釈を、
  // このflag専用に簡略化して再現する(OpenFeature SDK相当のフル機能は不要なため直接SELECTする)
  def readBoolFlag(flagKey: String): IO[Boolean] = {
    case class FlagRow(enabled: Boolean, defaultVariation: String, variations: Option[String])
    sql"SELECT enabled, default_variation, variations FROM feature_flags WHERE flag_key = $flagKey"
      .query[FlagRow]
      .option
      .transact(xa)
      .map {
        case None                       => false
        case Some(row) if !row.enabled  => false
        case Some(row) =>
          import io.circe.parser.parse
          val variations: Map[String, Boolean] = row.variations.filter(_.nonEmpty) match {
            case Some(v) =>
              parse(v).toOption
                .flatMap(_.asObject)
                .map(_.toMap.view.mapValues(_.asBoolean.getOrElse(false)).toMap)
                .getOrElse(Map("on" -> true, "off" -> false))
            case None => Map("on" -> true, "off" -> false)
          }
          variations.getOrElse(row.defaultVariation, false)
      }
  }

  // backend/internal/repository/user.go の Get/GetByKeycloakSub をそのまま再現する
  // keycloak_subはusersではなくuser_keycloaksテーブルにある(セクション16.2でのスキーマ変更)ため、
  // Keycloak発行トークンの解決だけJOINが必要になる
  def findUserById(id: Long): IO[Option[User]] =
    sql"SELECT id, email, name, role FROM users WHERE id = $id"
      .query[User]
      .option
      .transact(xa)

  def findUserByKeycloakSub(sub: String): IO[Option[User]] =
    sql"""SELECT users.id, users.email, users.name, users.role
          FROM users
          JOIN user_keycloaks ON user_keycloaks.user_id = users.id
          WHERE user_keycloaks.keycloak_sub = $sub"""
      .query[User]
      .option
      .transact(xa)
}
