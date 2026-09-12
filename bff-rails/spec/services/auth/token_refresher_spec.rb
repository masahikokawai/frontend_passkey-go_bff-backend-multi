require "rails_helper"

RSpec.describe Auth::TokenRefresher do
  describe ".refresh" do
    it "成功時、新しいaccess_token/refresh_tokenを返す" do
      stub_request(:post, "#{AppConfig.keycloak_issuer}/protocol/openid-connect/token")
        .with(body: hash_including("grant_type" => "refresh_token", "refresh_token" => "old-rt"))
        .to_return(status: 200, body: { access_token: "new-at", refresh_token: "new-rt" }.to_json)

      result = described_class.refresh("old-rt")

      expect(result).to eq(access_token: "new-at", refresh_token: "new-rt")
    end

    # 【WHY】KeycloakはリフレッシュのたびにRefresh Token Rotationで新しいrefresh_tokenを
    # 発行しない設定もありうる(レスポンスにrefresh_tokenが含まれない場合がある)。
    # その場合は元のrefresh_tokenを使い続けられるようフォールバックすることを確認する
    it "レスポンスにrefresh_tokenが含まれない場合、元のrefresh_tokenを維持する" do
      stub_request(:post, "#{AppConfig.keycloak_issuer}/protocol/openid-connect/token")
        .to_return(status: 200, body: { access_token: "new-at" }.to_json)

      result = described_class.refresh("old-rt")

      expect(result).to eq(access_token: "new-at", refresh_token: "old-rt")
    end

    it "Keycloakがrefresh_token失効で400を返した場合、Errorを送出する" do
      stub_request(:post, "#{AppConfig.keycloak_issuer}/protocol/openid-connect/token")
        .to_return(status: 400, body: { error: "invalid_grant" }.to_json)

      expect { described_class.refresh("expired-rt") }.to raise_error(described_class::Error, /status=400/)
    end

    it "Keycloakへの接続自体が失敗した場合もErrorに包んで送出する" do
      stub_request(:post, "#{AppConfig.keycloak_issuer}/protocol/openid-connect/token")
        .to_raise(Faraday::ConnectionFailed)

      expect { described_class.refresh("rt") }.to raise_error(described_class::Error, /token refresh request failed/)
    end
  end
end
