require "rails_helper"

RSpec.describe OidcGemFlag do
  after { OidcGemFlag.override!(nil) }

  describe ".current" do
    it "override!していなければ既定値(omniauth-openid-connect)を返す" do
      expect(OidcGemFlag.current).to eq("omniauth-openid-connect")
    end

    it "override!した値を返す" do
      OidcGemFlag.override!("openid_connect")
      expect(OidcGemFlag.current).to eq("openid_connect")
    end
  end

  describe ".poll_once" do
    let(:export_url) { "http://localhost:8090/internal/v1/feature-flags/export" }

    it "exportエンドポイントのdefaultRule.variationを反映する" do
      stub_request(:get, export_url)
        .with(headers: { "X-Feature-Flag-Poll-Token" => "local-dev-feature-flag-poll-token" })
        .to_return(status: 200, body: {
          "frontend-rails.oidc-gem" => {
            "variations" => { "omniauth-openid-connect" => "omniauth-openid-connect", "openid_connect" => "openid_connect" },
            "defaultRule" => { "variation" => "openid_connect" },
            "disable" => false,
          },
        }.to_json, headers: { "Content-Type" => "application/json" })

      OidcGemFlag.poll_once
      expect(OidcGemFlag.current).to eq("openid_connect")
    end

    it "disable=trueの場合は既定値にフォールバックする" do
      stub_request(:get, export_url).to_return(status: 200, body: {
        "frontend-rails.oidc-gem" => {
          "defaultRule" => { "variation" => "openid_connect" },
          "disable" => true,
        },
      }.to_json, headers: { "Content-Type" => "application/json" })

      OidcGemFlag.poll_once
      expect(OidcGemFlag.current).to eq("omniauth-openid-connect")
    end

    it "未知の値が返ってきた場合は既定値にフォールバックする" do
      stub_request(:get, export_url).to_return(status: 200, body: {
        "frontend-rails.oidc-gem" => { "defaultRule" => { "variation" => "some-future-gem" }, "disable" => false },
      }.to_json, headers: { "Content-Type" => "application/json" })

      OidcGemFlag.poll_once
      expect(OidcGemFlag.current).to eq("omniauth-openid-connect")
    end

    it "exportエンドポイントに到達できない場合は既定値にフォールバックする(既存の値も保持しない)" do
      stub_request(:get, export_url).to_timeout

      OidcGemFlag.poll_once
      expect(OidcGemFlag.current).to eq("omniauth-openid-connect")
    end

    it "一度取得できていれば、次の失敗時はその値を保持する" do
      stub_request(:get, export_url).to_return(status: 200, body: {
        "frontend-rails.oidc-gem" => { "defaultRule" => { "variation" => "openid_connect" }, "disable" => false },
      }.to_json, headers: { "Content-Type" => "application/json" })
      OidcGemFlag.poll_once
      expect(OidcGemFlag.current).to eq("openid_connect")

      stub_request(:get, export_url).to_timeout
      OidcGemFlag.poll_once
      expect(OidcGemFlag.current).to eq("openid_connect")
    end
  end
end
