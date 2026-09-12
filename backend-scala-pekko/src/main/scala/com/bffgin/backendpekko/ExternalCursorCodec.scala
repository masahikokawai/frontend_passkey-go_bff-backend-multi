package com.bffgin.backendpekko

import java.time.Instant
import java.time.format.DateTimeFormatter
import java.util.Base64
import scala.util.Try

// backend/internal/service/task_external.go の encodeExternalCursor/decodeExternalCursor に対応
// 中身は "<RFC3339Nano相当>|<id>" をbase64url化しただけの不透明(opaque)文字列
object ExternalCursorCodec {
  private val fmt = DateTimeFormatter.ISO_INSTANT

  def encode(c: ExternalCursor): String = {
    val raw = s"${fmt.format(c.createdAt)}|${c.id}"
    Base64.getUrlEncoder.encodeToString(raw.getBytes("UTF-8"))
  }

  def decode(cursor: String): Either[String, ExternalCursor] =
    Try {
      val raw = new String(Base64.getUrlDecoder.decode(cursor), "UTF-8")
      raw.split("\\|", 2) match {
        case Array(instantStr, idStr) =>
          ExternalCursor(Instant.parse(instantStr), idStr.toLong)
        case _ => throw new IllegalArgumentException("cursorの区切りが不正です")
      }
    }.toEither.left.map(_.getMessage)
}
