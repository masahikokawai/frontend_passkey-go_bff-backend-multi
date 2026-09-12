package com.bffgin.backend

import java.time.{LocalDate, LocalDateTime}

// backend/internal/model/enum.go の TaskStatus(waiting=1, work_in_progress=2, completed=3)を再現する
sealed abstract class TaskStatus(val code: Int, val wire: String)
object TaskStatus {
  case object Waiting extends TaskStatus(1, "waiting")
  case object WorkInProgress extends TaskStatus(2, "work_in_progress")
  case object Completed extends TaskStatus(3, "completed")

  val all: List[TaskStatus] = List(Waiting, WorkInProgress, Completed)

  def fromWire(s: String): Either[String, TaskStatus] =
    all.find(_.wire == s).toRight(s"不明なstatus: $s")

  def fromCode(c: Int): Either[String, TaskStatus] =
    all.find(_.code == c).toRight(s"不明なstatus code: $c")
}

final case class Label(id: Long, name: String)

final case class Task(
    id: Long,
    name: String,
    description: Option[String],
    status: TaskStatus,
    finishedOn: LocalDate,
    userId: Long,
    labels: List[Label],
    // MySQLの DATETIME(タイムゾーン無し)をそのまま LocalDateTime として保持し、
    // JSON出力時にUTCとみなしてRFC3339化する(Goのgo-sql-driver/mysqlがparseTime=true・
    // loc未指定のときUTCとして解釈するのに合わせる)
    createdAt: LocalDateTime,
    updatedAt: LocalDateTime
)

final case class TaskInput(
    name: String,
    description: Option[String],
    status: String,
    finishedOn: String,
    labelIds: List[Long]
)

// 【backend/internal/repository/user.go参照】keycloak_subはusersテーブルではなく
// user_keycloaksテーブルにあるため、ここには含めない(JOIN経由でのみ検索に使う)
final case class User(id: Long, email: String, name: String, role: Int)
