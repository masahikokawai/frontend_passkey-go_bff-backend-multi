package com.bffgin.backend.external

import munit.FunSuite
import java.time.LocalDateTime

class ExternalJsonSuite extends FunSuite {
  test("encodeCursor/decodeCursorが往復する") {
    val createdAt = LocalDateTime.of(2026, 9, 8, 21, 52, 41)
    val encoded    = ExternalJson.encodeCursor(createdAt, 267L)
    val decoded    = ExternalJson.decodeCursor(encoded)
    assertEquals(decoded, Some((createdAt, 267L)))
  }

  test("decodeCursorは不正なbase64・区切り不正・数値変換不能をNoneにする") {
    assertEquals(ExternalJson.decodeCursor("!!!not-base64!!!"), None)
    assertEquals(ExternalJson.decodeCursor(java.util.Base64.getUrlEncoder.encodeToString("no-separator".getBytes)), None)
    assertEquals(
      ExternalJson.decodeCursor(
        java.util.Base64.getUrlEncoder.encodeToString("2026-09-08T21:52:41|not-a-number".getBytes)
      ),
      None
    )
  }

  test("listV1Responseの形状") {
    val json = ExternalJson.listV1Response(Nil, page = 2, pageSize = 10, total = 5L)
    assertEquals(json.hcursor.get[Int]("page"), Right(2))
    assertEquals(json.hcursor.get[Int]("page_size"), Right(10))
    assertEquals(json.hcursor.get[Long]("total"), Right(5L))
  }

  test("listV2Responseはnext_cursor無しならnull") {
    val json = ExternalJson.listV2Response(Nil, None, limit = 10)
    assert(json.hcursor.downField("next_cursor").focus.exists(_.isNull))
  }
}
