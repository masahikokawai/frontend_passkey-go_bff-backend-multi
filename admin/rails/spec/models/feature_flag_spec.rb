require "rails_helper"

RSpec.describe FeatureFlag, type: :model do
  it "flag_keyが無いと無効" do
    flag = FactoryBot.build(:feature_flag, flag_key: nil)
    expect(flag).not_to be_valid
  end

  it "default_variationがon/off以外だと無効" do
    flag = FactoryBot.build(:feature_flag, default_variation: "maybe")
    expect(flag).not_to be_valid
  end

  it "on/offなら有効" do
    expect(FactoryBot.build(:feature_flag, default_variation: "on")).to be_valid
    expect(FactoryBot.build(:feature_flag, default_variation: "off")).to be_valid
  end

  it "flag_keyは一意" do
    FactoryBot.create(:feature_flag, flag_key: "dup.flag")
    dup = FactoryBot.build(:feature_flag, flag_key: "dup.flag")
    expect(dup).not_to be_valid
  end

  it "feature_flag_audit_logsを複数持てる" do
    flag = FactoryBot.create(:feature_flag)
    log = FeatureFlagAuditLog.create!(
      feature_flag_id: flag.id,
      flag_key: flag.flag_key,
      after_default_variation: "on",
      after_enabled: true,
      changed_by: "admin",
      changed_at: Time.current
    )
    expect(flag.feature_flag_audit_logs).to include(log)
  end

  it "複数回更新した場合、audit_logsは新しい順(changed_at desc)で取得できる(admin/goのListAuditLogs_OrdersNewestFirstと同じ観点、FeatureFlagsController#audit_logsが同じorderを使っている)" do
    flag = FactoryBot.create(:feature_flag)
    old_log = FeatureFlagAuditLog.create!(
      feature_flag_id: flag.id, flag_key: flag.flag_key,
      after_default_variation: "on", after_enabled: true,
      changed_by: "admin", changed_at: 2.hours.ago
    )
    new_log = FeatureFlagAuditLog.create!(
      feature_flag_id: flag.id, flag_key: flag.flag_key,
      after_default_variation: "off", after_enabled: true,
      changed_by: "admin", changed_at: 1.hour.ago
    )

    ordered = flag.feature_flag_audit_logs.order(changed_at: :desc)

    expect(ordered.to_a).to eq([new_log, old_log])
  end

  # CONTRACT.mdセクション19: 多値(multivariate)フラグ対応
  # admin/goのTestFeatureFlag_*と対になる観点
  describe "多値フラグ(variationsカラム)" do
    it "variations未設定(NULL)の場合、variation_optionsは既存フラグ相当の[off, on]にフォールバックする" do
      flag = FactoryBot.build(:feature_flag, variations: nil)
      expect(flag.variation_options).to contain_exactly("off", "on")
      expect(flag.boolean_toggle?).to be true
    end

    it "variationsが{on:true,off:false}の場合、boolean_toggle?はtrue" do
      flag = FactoryBot.build(:feature_flag, variations: { "on" => true, "off" => false })
      expect(flag.boolean_toggle?).to be true
    end

    it "variationsが3値(inline/modal/page)の場合、boolean_toggle?はfalseでvariation_optionsは3つとも返す" do
      flag = FactoryBot.build(:feature_flag, variations: { "inline" => "inline", "modal" => "modal", "page" => "page" })
      expect(flag.boolean_toggle?).to be false
      expect(flag.variation_options).to contain_exactly("inline", "modal", "page")
    end

    it "3値フラグでvariationsに存在しない値を指定すると無効" do
      flag = FactoryBot.build(:feature_flag,
        default_variation: "maybe",
        variations: { "inline" => "inline", "modal" => "modal", "page" => "page" })
      expect(flag).not_to be_valid
    end

    it "3値フラグでvariationsに存在する値を指定すると有効" do
      flag = FactoryBot.build(:feature_flag,
        default_variation: "modal",
        variations: { "inline" => "inline", "modal" => "modal", "page" => "page" })
      expect(flag).to be_valid
    end
  end
end
