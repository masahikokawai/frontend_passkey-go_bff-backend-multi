package com.bffgin.backendpekko

import slick.jdbc.MySQLProfile.api._

import java.sql.{Date => SqlDate, Timestamp}

// backend/migrations/000001〜000004 の users/tasks/labels/task_labels テーブルの写し
// このプロジェクトの既存方針(admin/go・admin/rails・他言語backend実装は独自マイグレーションを
// 持たず、backendのgolang-migrateが正本管理するこの既存テーブルへ接続するだけ)を踏襲する

final case class TaskRow(
    id: Long,
    name: String,
    description: Option[String],
    status: Int,
    finishedOn: SqlDate,
    userId: Long,
    createdAt: Timestamp,
    updatedAt: Timestamp
)
final class TasksTable(tag: Tag) extends Table[TaskRow](tag, "tasks") {
  def id = column[Long]("id", O.PrimaryKey, O.AutoInc)
  def name = column[String]("name")
  def description = column[Option[String]]("description")
  def status = column[Int]("status")
  def finishedOn = column[SqlDate]("finished_on")
  def userId = column[Long]("user_id")
  def createdAt = column[Timestamp]("created_at")
  def updatedAt = column[Timestamp]("updated_at")
  def * = (id, name, description, status, finishedOn, userId, createdAt, updatedAt).mapTo[TaskRow]
}

final case class LabelRow(id: Long, name: String, createdAt: Timestamp, updatedAt: Timestamp)
final class LabelsTable(tag: Tag) extends Table[LabelRow](tag, "labels") {
  def id = column[Long]("id", O.PrimaryKey, O.AutoInc)
  def name = column[String]("name")
  def createdAt = column[Timestamp]("created_at")
  def updatedAt = column[Timestamp]("updated_at")
  def * = (id, name, createdAt, updatedAt).mapTo[LabelRow]
}

final case class TaskLabelRow(id: Long, taskId: Long, labelId: Long, createdAt: Timestamp, updatedAt: Timestamp)
final class TaskLabelsTable(tag: Tag) extends Table[TaskLabelRow](tag, "task_labels") {
  def id = column[Long]("id", O.PrimaryKey, O.AutoInc)
  def taskId = column[Long]("task_id")
  def labelId = column[Long]("label_id")
  def createdAt = column[Timestamp]("created_at")
  def updatedAt = column[Timestamp]("updated_at")
  def * = (id, taskId, labelId, createdAt, updatedAt).mapTo[TaskLabelRow]
}

// 【backend/migrations/000008_split_user_credentials.up.sqlで判明】usersテーブルは当初
// keycloak_subカラムを持っていたが、ローカル認証(HMAC/RSA)ユーザーの追加に伴い
// user_keycloaksテーブルへ切り出され、usersからkeycloak_subカラム自体が削除されている
// (Go実装のCONTRACT.mdセクション16.2参照)
// 当初000001のみを正解として読んだ結果、このテーブルの現在の実態を見誤っていた(ライブDBで実際に確認して修正した)
final case class UserRow(
    id: Long,
    email: String,
    name: String,
    role: Int,
    createdAt: Timestamp,
    updatedAt: Timestamp
)
final class UsersTable(tag: Tag) extends Table[UserRow](tag, "users") {
  def id = column[Long]("id", O.PrimaryKey, O.AutoInc)
  def email = column[String]("email")
  def name = column[String]("name")
  def role = column[Int]("role")
  def createdAt = column[Timestamp]("created_at")
  def updatedAt = column[Timestamp]("updated_at")
  def * = (id, email, name, role, createdAt, updatedAt).mapTo[UserRow]
}

final case class UserKeycloakRow(id: Long, userId: Long, keycloakSub: String, createdAt: Timestamp, updatedAt: Timestamp)
final class UserKeycloaksTable(tag: Tag) extends Table[UserKeycloakRow](tag, "user_keycloaks") {
  def id = column[Long]("id", O.PrimaryKey, O.AutoInc)
  def userId = column[Long]("user_id")
  def keycloakSub = column[String]("keycloak_sub")
  def createdAt = column[Timestamp]("created_at")
  def updatedAt = column[Timestamp]("updated_at")
  def * = (id, userId, keycloakSub, createdAt, updatedAt).mapTo[UserKeycloakRow]
}

// backend/migrations/000006_create_feature_flags.up.sql の写し
// CONTRACT.mdセクション11・20.7: 外部公開APIのページネーション方式切り替え
// (backend.external-tasks-pagination-v2)を、backend自身がこのテーブルから直接評価する
// (Go実装のinternal/featureflag/mysql_retriever.goと同じ考え方
// bff/gateway のような HTTP ポーリング経由ではなく、DBを正本として直接読む)
final case class FeatureFlagRow(
    id: Long,
    flagKey: String,
    defaultVariation: String,
    enabled: Boolean,
    variations: Option[String]
)
final class FeatureFlagsTable(tag: Tag) extends Table[FeatureFlagRow](tag, "feature_flags") {
  def id = column[Long]("id", O.PrimaryKey, O.AutoInc)
  def flagKey = column[String]("flag_key")
  def defaultVariation = column[String]("default_variation")
  def enabled = column[Boolean]("enabled")
  def variations = column[Option[String]]("variations")
  def * = (id, flagKey, defaultVariation, enabled, variations).mapTo[FeatureFlagRow]
}

object Tables {
  val tasks = TableQuery[TasksTable]
  val labels = TableQuery[LabelsTable]
  val taskLabels = TableQuery[TaskLabelsTable]
  val users = TableQuery[UsersTable]
  val userKeycloaks = TableQuery[UserKeycloaksTable]
  val featureFlags = TableQuery[FeatureFlagsTable]
}
