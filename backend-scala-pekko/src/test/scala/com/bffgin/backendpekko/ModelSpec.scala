package com.bffgin.backendpekko

import org.scalatest.matchers.should.Matchers
import org.scalatest.wordspec.AnyWordSpec

// backend/internal/model/enum.go の TaskStatus と対応関係が一致していることを確認する
class ModelSpec extends AnyWordSpec with Matchers {
  "TaskStatus" should {
    "waiting/work_in_progress/completedを1/2/3にマッピングする(Rails enumとの互換性)" in {
      TaskStatus.toInt("waiting") shouldBe Some(1)
      TaskStatus.toInt("work_in_progress") shouldBe Some(2)
      TaskStatus.toInt("completed") shouldBe Some(3)
    }

    "1/2/3をwaiting/work_in_progress/completedへ復元する" in {
      TaskStatus.fromInt(1) shouldBe "waiting"
      TaskStatus.fromInt(2) shouldBe "work_in_progress"
      TaskStatus.fromInt(3) shouldBe "completed"
    }

    "不明な値はunknown(N)を返す" in {
      TaskStatus.fromInt(99) shouldBe "unknown(99)"
    }

    "isValidは既知の3値のみtrueを返す" in {
      TaskStatus.isValid("waiting") shouldBe true
      TaskStatus.isValid("bogus") shouldBe false
      TaskStatus.isValid("") shouldBe false
    }
  }
}
