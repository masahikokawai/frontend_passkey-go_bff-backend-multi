package com.bffgin.backend.external

import com.bffgin.backend._
import io.circe.{Json => CJson}
import java.time.{LocalDateTime, ZoneOffset}
import java.time.format.DateTimeFormatter
import java.util.Base64
import scala.util.Try

// CONTRACT.mdセクション11: 外部公開APIのJSON形状は内部CRUD(Json.scala)とは別物
// (最大の違いはuser_idを含めない点。Goのbackend/internal/handler/external/task.goと同じ)
object ExternalJson {
  private def rfc3339(ldt: LocalDateTime): String =
    ldt.atOffset(ZoneOffset.UTC).format(DateTimeFormatter.ISO_OFFSET_DATE_TIME)

  def taskEncoder(t: Task): CJson =
    CJson.obj(
      "id"          -> CJson.fromLong(t.id),
      "name"        -> CJson.fromString(t.name),
      "description" -> t.description.fold(CJson.Null)(CJson.fromString),
      "status"      -> CJson.fromString(t.status.wire),
      "finished_on" -> CJson.fromString(t.finishedOn.toString),
      "labels" -> CJson.fromValues(
        t.labels.map(l => CJson.obj("id" -> CJson.fromLong(l.id), "name" -> CJson.fromString(l.name)))
      ),
      "created_at" -> CJson.fromString(rfc3339(t.createdAt)),
      "updated_at" -> CJson.fromString(rfc3339(t.updatedAt))
    )

  def listV1Response(tasks: List[Task], page: Int, pageSize: Int, total: Long): CJson =
    CJson.obj(
      "tasks"     -> CJson.fromValues(tasks.map(taskEncoder)),
      "page"      -> CJson.fromInt(page),
      "page_size" -> CJson.fromInt(pageSize),
      "total"     -> CJson.fromLong(total)
    )

  def listV2Response(tasks: List[Task], nextCursor: Option[String], limit: Int): CJson =
    CJson.obj(
      "tasks"       -> CJson.fromValues(tasks.map(taskEncoder)),
      "next_cursor" -> nextCursor.fold(CJson.Null)(CJson.fromString),
      "limit"       -> CJson.fromInt(limit)
    )

  def errorJson(code: String): CJson = CJson.obj("error" -> CJson.fromString(code))

  // opaque cursor: base64url("<ISO-8601 timestamp>|<id>")。各backendが自分でencode/decodeする
  // だけの内部往復用途のため、Go実装とバイト単位で一致させる必要は無い(往復さえできればよい)
  def encodeCursor(createdAt: LocalDateTime, id: Long): String = {
    val raw = s"${createdAt.toString}|$id"
    Base64.getUrlEncoder.encodeToString(raw.getBytes("UTF-8"))
  }

  def decodeCursor(cursor: String): Option[(LocalDateTime, Long)] =
    Try {
      val raw   = new String(Base64.getUrlDecoder.decode(cursor), "UTF-8")
      val parts = raw.split("\\|", 2)
      (LocalDateTime.parse(parts(0)), parts(1).toLong)
    }.toOption
}
