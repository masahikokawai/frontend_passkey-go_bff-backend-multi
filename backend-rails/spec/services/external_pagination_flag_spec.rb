require "rails_helper"

# backend/internal/featureflag/mysql_retriever.go 相当の Rails 版
# backend.external-tasks-pagination-v2 が5言語で共有する1つのflagであることの前提
# (CONTRACT.mdセクション11・20.9)を、このアプリの読み取りロジック側でも確認する
RSpec.describe ExternalPaginationFlag do
  before { described_class.reset! }

  after { described_class.reset! }

  it "enabled=true かつ default_variation=on ならtrue" do
    FeatureFlag.create!(flag_key: "backend.external-tasks-pagination-v2", default_variation: "on", enabled: true)
    expect(described_class.v2_enabled?).to be true
  end

  it "enabled=true かつ default_variation=off ならfalse" do
    FeatureFlag.create!(flag_key: "backend.external-tasks-pagination-v2", default_variation: "off", enabled: true)
    expect(described_class.v2_enabled?).to be false
  end

  it "enabled=false なら default_variation の値に関わらずfalse(Go実装のdisable扱いと同一)" do
    FeatureFlag.create!(flag_key: "backend.external-tasks-pagination-v2", default_variation: "on", enabled: false)
    expect(described_class.v2_enabled?).to be false
  end

  it "行自体が存在しなければfalse" do
    expect(described_class.v2_enabled?).to be false
  end

  it "ポーリング間隔内はDBの変更を再取得しない(キャッシュされる)" do
    FeatureFlag.create!(flag_key: "backend.external-tasks-pagination-v2", default_variation: "off", enabled: true)
    expect(described_class.v2_enabled?).to be false

    FeatureFlag.find_by(flag_key: "backend.external-tasks-pagination-v2").update!(default_variation: "on")
    expect(described_class.v2_enabled?).to be false # まだキャッシュされたfalseのまま

    described_class.reset!
    expect(described_class.v2_enabled?).to be true # reset後は最新値を読む
  end
end
