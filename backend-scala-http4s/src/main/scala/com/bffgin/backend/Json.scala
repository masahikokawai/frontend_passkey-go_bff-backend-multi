package com.bffgin.backend

import io.circe.{Decoder, Encoder, Json => CJson}
import io.circe.generic.semiauto._
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

// CONTRACT.mdセクション5.1のJSON形状(bff側のtaskV1DTO/taskV1RequestBodyと同じスネークケース)に一致させる
// ここをcamelCaseにすると、bffのresty呼び出しがfinished_on/created_at/updated_atを読めず空文字になる
// (既存Go実装で実際に踏んだ不整合、CONTRACT.md参照)
object Json {
  private def rfc3339(ldt: java.time.LocalDateTime): String =
    ldt.atOffset(ZoneOffset.UTC).format(DateTimeFormatter.ISO_OFFSET_DATE_TIME)

  implicit val labelEncoder: Encoder[Label] = deriveEncoder

  implicit val taskEncoder: Encoder[Task] = Encoder.instance { t =>
    CJson.obj(
      "id"          -> CJson.fromLong(t.id),
      "name"        -> CJson.fromString(t.name),
      "description" -> t.description.fold(CJson.Null)(CJson.fromString),
      "status"      -> CJson.fromString(t.status.wire),
      "finished_on" -> CJson.fromString(t.finishedOn.toString),
      "user_id"     -> CJson.fromLong(t.userId),
      "labels"      -> CJson.fromValues(t.labels.map(labelEncoder.apply)),
      "created_at"  -> CJson.fromString(rfc3339(t.createdAt)),
      "updated_at"  -> CJson.fromString(rfc3339(t.updatedAt))
    )
  }

  final case class TaskRequestBody(
      name: Option[String],
      description: Option[String],
      status: Option[String],
      finished_on: Option[String],
      label_ids: Option[List[Long]]
  )
  implicit val taskRequestBodyDecoder: Decoder[TaskRequestBody] = deriveDecoder

  def errorJson(code: String): CJson = CJson.obj("error" -> CJson.fromString(code))
  def errorJsonWithMessage(code: String, message: String): CJson =
    CJson.obj("error" -> CJson.fromString(code), "message" -> CJson.fromString(message))

  def listResponse(tasks: List[Task], total: Long, limit: Int, offset: Int): CJson =
    CJson.obj(
      "tasks"  -> CJson.fromValues(tasks.map(taskEncoder.apply)),
      "total"  -> CJson.fromLong(total),
      "limit"  -> CJson.fromInt(limit),
      "offset" -> CJson.fromInt(offset)
    )
}
