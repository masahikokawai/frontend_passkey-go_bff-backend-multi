require "rails_helper"

RSpec.describe FeatureFlagClient do
  before { described_class.reset! }

  it "backendのexportレスポンスからdefaultRule.variationを返す" do
    stub_request(:get, AppConfig.feature_flag_export_url)
      .with(headers: { "X-Feature-Flag-Poll-Token" => AppConfig.feature_flag_poll_token })
      .to_return(
        status: 200,
        body: { "frontend-rails.oidc-gem" => { "defaultRule" => { "variation" => "openid_connect" } } }.to_json
      )

    expect(described_class.oidc_gem).to eq("openid_connect")
  end

  it "backendに到達できない場合は既定値にフォールバックする" do
    stub_request(:get, AppConfig.feature_flag_export_url).to_timeout

    expect(described_class.oidc_gem).to eq(described_class::DEFAULT_VARIATION)
  end

  it "フラグが見つからない場合は既定値にフォールバックする" do
    stub_request(:get, AppConfig.feature_flag_export_url)
      .to_return(status: 200, body: { "other.flag" => { "defaultRule" => { "variation" => "x" } } }.to_json)

    expect(described_class.oidc_gem).to eq(described_class::DEFAULT_VARIATION)
  end

  # 【テスト監査で追記】既存のフォールバックテストはタイムアウト(例外)のみを検証していたが、
  # fetch_valueには`resp.success?`がfalseの場合にnilを返す、例外を投げない別経路がある
  # (backendが200以外を返す場合、例: 一時的な500)。この経路は例外を投げないため
  # rescue節を経由せず`@value = fetch_value || @value`にそのまま合流するが、
  # 挙動としては同じく既定値へのフォールバックになるはず、という点を別テストとして明示する
  it "backendが200以外を返す場合も既定値にフォールバックする(例外を投げない経路)" do
    stub_request(:get, AppConfig.feature_flag_export_url).to_return(status: 500, body: "")

    expect(described_class.oidc_gem).to eq(described_class::DEFAULT_VARIATION)
  end

  it "backendのレスポンスがJSONとして不正な場合も既定値にフォールバックする(JSON::ParserErrorがStandardErrorとしてrescueされる経路)" do
    stub_request(:get, AppConfig.feature_flag_export_url).to_return(status: 200, body: "not-json{{{")

    expect(described_class.oidc_gem).to eq(described_class::DEFAULT_VARIATION)
  end

  it "poll_interval秒以内の再アクセスはbackendへ再度問い合わせない(遅延評価キャッシュ)" do
    stub = stub_request(:get, AppConfig.feature_flag_export_url)
      .to_return(status: 200, body: { "frontend-rails.oidc-gem" => { "defaultRule" => { "variation" => "openid_connect" } } }.to_json)

    2.times { described_class.oidc_gem }

    expect(stub).to have_been_requested.once
  end
end
