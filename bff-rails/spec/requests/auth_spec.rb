require "rails_helper"

RSpec.describe "AuthController", type: :request do
  before do
    FeatureFlagClient.reset!
  end

  describe "GET /api/auth/login" do
    %w[omniauth-openid-connect openid_connect].each do |gem_name|
      it "flag=#{gem_name}のとき、PKCE付きでKeycloakの認可エンドポイントへリダイレクトする" do
        stub_request(:get, AppConfig.feature_flag_export_url)
          .to_return(status: 200, body: { "frontend-rails.oidc-gem" => { "defaultRule" => { "variation" => gem_name } } }.to_json)

        get "/api/auth/login"

        expect(response).to have_http_status(:found)
        location = URI.parse(response.headers["Location"])
        params = Rack::Utils.parse_query(location.query)
        expect(location.to_s).to start_with("#{AppConfig.keycloak_issuer}/protocol/openid-connect/auth")
        expect(params["client_id"]).to eq(AppConfig.keycloak_client_id)
        expect(params["code_challenge_method"]).to eq("S256")
        expect(params["code_challenge"]).to be_present
        expect(params["state"]).to be_present
        expect(params["nonce"]).to be_present
      end
    end
  end

  describe "GET /auth/openid_connect/callback" do
    %w[omniauth-openid-connect openid_connect].each do |gem_name|
      it "flag=#{gem_name}のとき、コード交換・JITプロビジョニング・セッション発行まで成功する" do
        pending = Auth::PendingLoginStore.create(gem: gem_name)
        id_token, jwks = build_signed_id_token(sub: "keycloak-sub-1", nonce: pending.nonce)

        stub_request(:post, "#{AppConfig.keycloak_issuer}/protocol/openid-connect/token")
          .to_return(
            status: 200,
            headers: { "Content-Type" => "application/json" },
            body: {
              access_token: "test-access-token",
              refresh_token: "test-refresh-token",
              id_token: id_token,
              token_type: "Bearer",
              expires_in: 300
            }.to_json
          )
        stub_request(:get, "#{AppConfig.keycloak_issuer}/protocol/openid-connect/certs")
          .to_return(status: 200, body: jwks.to_json)
        stub_request(:post, "#{AppConfig.backend_rest_base_url}/internal/v1/users/provision")
          .with(headers: { "Authorization" => "Bearer test-access-token" })
          .to_return(status: 200, body: { user_id: 1, role: "general" }.to_json)

        get "/auth/openid_connect/callback", params: { state: pending.state, code: "auth-code-123" }

        expect(response).to have_http_status(:found)
        expect(response.headers["Location"]).to eq(AppConfig.frontend_origin)
        session_cookie = response.cookies["bff_rails_session"]
        expect(session_cookie).to be_present

        session = SessionStore.find(session_cookie)
        expect(session.keycloak_sub).to eq("keycloak-sub-1")
        expect(session.name).to eq("Taro Yamada")
        expect(session.access_token).to eq("test-access-token")
      end
    end

    it "stateが不正/期限切れの場合は401を返す" do
      get "/auth/openid_connect/callback", params: { state: "unknown-state", code: "x" }
      expect(response).to have_http_status(:unauthorized)
    end

    it "Keycloak側がerrorを返した場合は502(bad_gateway)で返す" do
      get "/auth/openid_connect/callback", params: { error: "access_denied", error_description: "user cancelled" }
      expect(response).to have_http_status(:bad_gateway)
    end
  end

  describe "POST /api/auth/logout" do
    it "セッションを削除し、Keycloakのlogout URLを返す" do
      session = SessionStore::Session.new(keycloak_sub: "s", name: "n", email: "e", access_token: "a", refresh_token: "r")
      id = SessionStore.create(session)
      cookies["bff_rails_session"] = id

      post "/api/auth/logout"

      expect(response).to have_http_status(:ok)
      expect(JSON.parse(response.body)["logout_url"]).to include("protocol/openid-connect/logout")
      expect(SessionStore.find(id)).to be_nil
    end
  end
end
