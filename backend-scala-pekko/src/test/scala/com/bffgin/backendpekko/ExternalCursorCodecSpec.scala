package com.bffgin.backendpekko

import org.scalatest.matchers.should.Matchers
import org.scalatest.wordspec.AnyWordSpec

import java.time.Instant

class ExternalCursorCodecSpec extends AnyWordSpec with Matchers {
  "encode/decode" should {
    "往復してもとの値に戻る" in {
      val original = ExternalCursor(Instant.parse("2026-09-08T12:34:56.789Z"), 42L)
      val encoded = ExternalCursorCodec.encode(original)
      ExternalCursorCodec.decode(encoded) shouldBe Right(original)
    }

    "不正なbase64はLeftを返す" in {
      ExternalCursorCodec.decode("!!!not-base64!!!").isLeft shouldBe true
    }

    "区切り文字が無いとLeftを返す" in {
      val bogus = java.util.Base64.getUrlEncoder.encodeToString("no-separator-here".getBytes("UTF-8"))
      ExternalCursorCodec.decode(bogus).isLeft shouldBe true
    }
  }
}
