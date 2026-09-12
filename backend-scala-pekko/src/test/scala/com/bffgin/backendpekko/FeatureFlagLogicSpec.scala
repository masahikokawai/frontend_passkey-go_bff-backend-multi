package com.bffgin.backendpekko

import org.scalatest.matchers.should.Matchers
import org.scalatest.wordspec.AnyWordSpec

// CONTRACT.mdセクション11・20.7: backend.external-tasks-pagination-v2の評価規則が
// backend/internal/featureflag/mysql_retriever.go のBuildFlagConfigJSONと同じであることを確認する
class FeatureFlagLogicSpec extends AnyWordSpec with Matchers {
  "resolveBool" should {
    "行が無ければdefaultValueを返す" in {
      FeatureFlagLogic.resolveBool(None, defaultValue = false) shouldBe false
      FeatureFlagLogic.resolveBool(None, defaultValue = true) shouldBe true
    }

    "enabled=falseならdefaultValueを返す(variationsの中身に関わらず)" in {
      val row = FeatureFlagRow(1, "backend.external-tasks-pagination-v2", "on", enabled = false, Some("""{"on":true,"off":false}"""))
      FeatureFlagLogic.resolveBool(Some(row), defaultValue = false) shouldBe false
    }

    "enabled=trueかつdefault_variation=onならvariationsのon値(true)を返す" in {
      val row = FeatureFlagRow(1, "backend.external-tasks-pagination-v2", "on", enabled = true, Some("""{"on":true,"off":false}"""))
      FeatureFlagLogic.resolveBool(Some(row), defaultValue = false) shouldBe true
    }

    "enabled=trueかつdefault_variation=offならfalseを返す" in {
      val row = FeatureFlagRow(1, "backend.external-tasks-pagination-v2", "off", enabled = true, Some("""{"on":true,"off":false}"""))
      FeatureFlagLogic.resolveBool(Some(row), defaultValue = true) shouldBe false
    }

    "variationsがNone(古い行)なら既定の{on:true,off:false}にフォールバックする" in {
      val row = FeatureFlagRow(1, "backend.external-tasks-pagination-v2", "on", enabled = true, None)
      FeatureFlagLogic.resolveBool(Some(row), defaultValue = false) shouldBe true
    }

    "variationsが不正なJSONならdefaultBooleanVariationsにフォールバックする" in {
      val row = FeatureFlagRow(1, "backend.external-tasks-pagination-v2", "on", enabled = true, Some("not json"))
      FeatureFlagLogic.resolveBool(Some(row), defaultValue = false) shouldBe true
    }

    "default_variationがvariationsのキーに無ければdefaultValueにフォールバックする" in {
      val row = FeatureFlagRow(1, "backend.external-tasks-pagination-v2", "unknown", enabled = true, Some("""{"on":true,"off":false}"""))
      FeatureFlagLogic.resolveBool(Some(row), defaultValue = false) shouldBe false
      FeatureFlagLogic.resolveBool(Some(row), defaultValue = true) shouldBe true
    }
  }
}
