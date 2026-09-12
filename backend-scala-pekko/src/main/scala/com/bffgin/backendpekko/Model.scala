package com.bffgin.backendpekko

import java.time.{Instant, LocalDate}

// backend/internal/model/enum.go の TaskStatus(Rails enum status: { waiting: 1,
// work_in_progress: 2, completed: 3 })と完全に同じ対応関係を再現する
object TaskStatus {
  val Waiting = "waiting"
  val WorkInProgress = "work_in_progress"
  val Completed = "completed"

  private val toIntMap: Map[String, Int] =
    Map(Waiting -> 1, WorkInProgress -> 2, Completed -> 3)
  private val fromIntMap: Map[Int, String] = toIntMap.map(_.swap)

  def toInt(s: String): Option[Int] = toIntMap.get(s)
  def fromInt(i: Int): String = fromIntMap.getOrElse(i, s"unknown($i)")
  def isValid(s: String): Boolean = toIntMap.contains(s)
}

final case class LabelDTO(id: Long, name: String)

// backend/internal/service/task.go の TaskDTO に対応(REST/gRPC共通の内部表現)
final case class TaskDTO(
    id: Long,
    name: String,
    description: Option[String],
    status: String,
    finishedOn: LocalDate,
    userId: Long,
    labels: Seq[LabelDTO],
    createdAt: Instant,
    updatedAt: Instant
)

// backend/internal/service/task.go の TaskInput に対応(Create/Update共通の入力)
final case class TaskInput(
    name: String,
    description: Option[String],
    status: String,
    finishedOn: LocalDate,
    labelIds: Seq[Long]
)

final case class UserDTO(id: Long, keycloakSub: String, email: String, name: String, role: Int)

sealed trait ServiceError
object ServiceError {
  case object NotFound extends ServiceError
  final case class Validation(message: String) extends ServiceError
  case object UserNotProvisioned extends ServiceError
}
