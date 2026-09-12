require "rails_helper"

RSpec.describe "TasksController", type: :request do
  describe "GET /api/tasks" do
    it "未ログイン(セッションCookie無し)なら401" do
      get "/api/tasks"
      expect(response).to have_http_status(:unauthorized)
    end

    it "ログイン中ならbackendの/internal/v1/tasksをそのまま透過して返す" do
      session = SessionStore::Session.new(
        keycloak_sub: "s", name: "n", email: "e", access_token: "user-access-token", refresh_token: "r"
      )
      id = SessionStore.create(session)
      cookies["bff_rails_session"] = id

      backend_body = { "tasks" => [{ "id" => 1, "name" => "test" }], "total" => 1, "limit" => 20, "offset" => 0 }
      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/tasks")
        .with(headers: { "Authorization" => "Bearer user-access-token" })
        .to_return(status: 200, body: backend_body.to_json)

      get "/api/tasks"

      expect(response).to have_http_status(:ok)
      expect(JSON.parse(response.body)).to eq(backend_body)
    end

    it "backendが5xx等の一般エラーを返した場合は502で返す" do
      session = SessionStore::Session.new(keycloak_sub: "s", name: "n", email: "e", access_token: "at", refresh_token: "r")
      id = SessionStore.create(session)
      cookies["bff_rails_session"] = id

      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/tasks")
        .to_return(status: 500, body: { error: "internal_server_error" }.to_json)

      get "/api/tasks"
      expect(response).to have_http_status(:bad_gateway)
    end

    # 【2回目のテスト監査で修正・追記】以前は403(user_not_provisioned等)も一律502として
    # 扱っていたが、bff(Go)のTaskRoutes.do()は同種のケース(ErrUpstreamUnauthorized/
    # ErrUserNotProvisioned)を401として扱っている(セクション17参照)。挙動をbffに揃え、
    # 「認証・認可に起因する失敗」と「その他のbackend障害」を区別するよう修正した
    it "backendが403(user_not_provisioned)を返し、refresh対象外(auth_mode未設定)ならセッションを破棄して401" do
      session = SessionStore::Session.new(keycloak_sub: "s", name: "n", email: "e", access_token: "at", refresh_token: "r")
      id = SessionStore.create(session)
      cookies["bff_rails_session"] = id

      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/tasks")
        .to_return(status: 403, body: { error: "user_not_provisioned" }.to_json)

      get "/api/tasks"
      expect(response).to have_http_status(:unauthorized)
      expect(SessionStore.find(id)).to be_nil
    end

    it "backendへの接続自体ができない場合、401ではなく502で返す(誤って再ログインを促さない)" do
      session = SessionStore::Session.new(keycloak_sub: "s", name: "n", email: "e", access_token: "at", refresh_token: "r")
      id = SessionStore.create(session)
      cookies["bff_rails_session"] = id

      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/tasks").to_raise(Faraday::ConnectionFailed)

      get "/api/tasks"
      expect(response).to have_http_status(:bad_gateway)
      expect(SessionStore.find(id)).not_to be_nil
    end

    # 【2回目のテスト監査で発見・修正した本題】Keycloakのaccess_tokenが期限切れ(401/403)でも
    # refresh_tokenを使って自動的に更新し、1回だけ再試行して成功させる
    it "auth_mode=keycloakでaccess_token期限切れなら、refreshして再試行し成功する" do
      session = SessionStore::Session.new(
        keycloak_sub: "s", name: "n", email: "e",
        access_token: "expired-token", refresh_token: "refresh-token", auth_mode: "keycloak"
      )
      id = SessionStore.create(session)
      cookies["bff_rails_session"] = id

      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/tasks")
        .with(headers: { "Authorization" => "Bearer expired-token" })
        .to_return(status: 401, body: { error: "unauthorized" }.to_json)
      stub_request(:post, "#{AppConfig.keycloak_issuer}/protocol/openid-connect/token")
        .with(body: hash_including("grant_type" => "refresh_token", "refresh_token" => "refresh-token"))
        .to_return(status: 200, body: { access_token: "new-token", refresh_token: "new-refresh" }.to_json)
      backend_body = { "tasks" => [], "total" => 0, "limit" => 20, "offset" => 0 }
      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/tasks")
        .with(headers: { "Authorization" => "Bearer new-token" })
        .to_return(status: 200, body: backend_body.to_json)

      get "/api/tasks"

      expect(response).to have_http_status(:ok)
      expect(JSON.parse(response.body)).to eq(backend_body)
      # セッションの中身がrefresh後の新しいトークンに更新され、同じsession_idのまま維持されていること
      refreshed = SessionStore.find(id)
      expect(refreshed.access_token).to eq("new-token")
      expect(refreshed.refresh_token).to eq("new-refresh")
    end

    it "auth_mode=keycloakでrefresh自体も失敗(refresh_token失効等)したら、セッションを破棄して401" do
      session = SessionStore::Session.new(
        keycloak_sub: "s", name: "n", email: "e",
        access_token: "expired-token", refresh_token: "dead-refresh-token", auth_mode: "keycloak"
      )
      id = SessionStore.create(session)
      cookies["bff_rails_session"] = id

      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/tasks")
        .to_return(status: 401, body: { error: "unauthorized" }.to_json)
      stub_request(:post, "#{AppConfig.keycloak_issuer}/protocol/openid-connect/token")
        .to_return(status: 400, body: { error: "invalid_grant" }.to_json)

      get "/api/tasks"

      expect(response).to have_http_status(:unauthorized)
      expect(SessionStore.find(id)).to be_nil
    end

    it "auth_mode=passkeyはrefresh_tokenの概念が無いため、401時にrefreshを試みずセッションを破棄する" do
      session = SessionStore::Session.new(
        keycloak_sub: nil, name: "n", email: "e",
        access_token: "local-jwt", refresh_token: nil, auth_mode: "passkey"
      )
      id = SessionStore.create(session)
      cookies["bff_rails_session"] = id

      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/tasks")
        .to_return(status: 401, body: { error: "unauthorized" }.to_json)

      get "/api/tasks"

      expect(response).to have_http_status(:unauthorized)
      expect(SessionStore.find(id)).to be_nil
      # Keycloakのtoken endpointへは一切リクエストが飛んでいないこと(refresh_token自体が無いため)
      expect(WebMock).not_to have_requested(:post, "#{AppConfig.keycloak_issuer}/protocol/openid-connect/token")
    end
  end
end
