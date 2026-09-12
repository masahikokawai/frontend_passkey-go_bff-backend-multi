require "rails_helper"
require "webauthn/fake_client"
require "base64"

RSpec.describe "PasskeysController", type: :request do
  # WebAuthn::FakeClientは実際に暗号学的に妥当なcreate/getレスポンスを生成できる
  # (webauthn gem自身がテスト用に提供しているヘルパー)。.verify()をモックせず、
  # 本物の検証ロジックを実際に通す
  let(:fake_client) { WebAuthn::FakeClient.new(AppConfig.webauthn_origin) }

  def login_session(user_id: 42, access_token: "user-access-token")
    session = SessionStore::Session.new(
      user_id: user_id, keycloak_sub: "sub-1", name: "Taro", email: "taro@example.com",
      access_token: access_token, refresh_token: "rt", auth_mode: "keycloak"
    )
    id = SessionStore.create(session)
    cookies["bff_rails_session"] = id
  end

  describe "POST /api/auth/passkey/register/begin" do
    it "未ログインなら401" do
      post "/api/auth/passkey/register/begin"
      expect(response).to have_http_status(:unauthorized)
    end

    it "ログイン中ならchallenge付きのoptionsとstateを返す" do
      login_session
      post "/api/auth/passkey/register/begin"
      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["state"]).to be_present
      expect(body["options"]["challenge"]).to be_present
      expect(body["options"]["authenticatorSelection"]["residentKey"]).to eq("required")
    end
  end

  describe "POST /api/auth/passkey/login/begin" do
    it "未ログインでも呼べ、allowCredentials指定無し(空)のoptionsを返す(discoverable credential)" do
      post "/api/auth/passkey/login/begin"
      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["state"]).to be_present
      expect(body["options"]["allowCredentials"]).to eq([])
    end
  end

  describe "登録→ログインの一連の流れ" do
    it "登録したパスキーで、Keycloakを経由せずログインできる(bff(Go)と共有する内部APIの契約通り)" do
      login_session(user_id: 7)

      post "/api/auth/passkey/register/begin"
      begin_body = JSON.parse(response.body)
      create_result = fake_client.create(challenge: begin_body["options"]["challenge"])

      captured = {}
      stub_request(:post, "#{AppConfig.backend_rest_base_url}/internal/v1/auth/webauthn/credentials")
        .with(headers: { "Authorization" => "Bearer user-access-token" })
        .to_return do |request|
          captured.merge!(JSON.parse(request.body))
          { status: 201, body: { id: 1 }.to_json }
        end

      post "/api/auth/passkey/register/finish",
        params: { state: begin_body["state"], credential: create_result }
      expect(response).to have_http_status(:ok)
      expect(JSON.parse(response.body)["registered"]).to eq(true)
      expect(captured["credential_id"]).to eq(create_result["id"])
      expect(captured["sign_count"]).to eq(0)

      # ここから先はKeycloakセッションを一切使わない、パスキーのみでのログイン
      post "/api/auth/passkey/login/begin"
      login_begin_body = JSON.parse(response.body)
      get_result = fake_client.get(challenge: login_begin_body["options"]["challenge"])

      # name/email/rolesもbackendのGET .../credentials/:idレスポンスに含まれる
      # (実際のbackend実装が返す形状、bffとの結合確認で追加されたフィールド)。
      # これらはAuth::LocalBackendTokenIssuerが自前JWTのクレームに詰めるために必要
      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/auth/webauthn/credentials/#{get_result['id']}")
        .to_return(status: 200, body: {
          user_id: 7, public_key: captured["public_key"], sign_count: captured["sign_count"],
          name: "パスキー太郎", email: "passkey-taro@example.com", roles: ["general"]
        }.to_json)
      sign_count_stub = stub_request(
        :patch, "#{AppConfig.backend_rest_base_url}/internal/v1/auth/webauthn/credentials/#{get_result['id']}/sign-count"
      ).to_return(status: 204)

      post "/api/auth/passkey/login/finish",
        params: { state: login_begin_body["state"], credential: get_result }

      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["logged_in"]).to eq(true)
      expect(body["auth_mode"]).to eq("passkey")
      expect(sign_count_stub).to have_been_requested

      session_id = response.cookies["bff_rails_session"]
      expect(session_id).to be_present
      session = SessionStore.find(session_id)
      expect(session.user_id.to_s).to eq("7")
      expect(session.auth_mode).to eq("passkey")

      # 【本題】パスキーログイン後もaccess_token(=bff(Go)と同じ形式の自前JWT)が
      # セッションに入っており、/api/tasksがそれをそのまま転送して動くことを確認する
      expect(session.access_token).to be_present
      decoded = JWT.decode(
        session.access_token, AppConfig.local_auth_hmac_secret, true, algorithm: "HS256"
      )
      claims = decoded.first
      expect(claims["iss"]).to eq("bff-gin-local-hmac")
      expect(claims["sub"]).to eq("7")
      expect(claims["aud"]).to eq(["backend"])
      expect(claims["name"]).to eq("パスキー太郎")
      expect(claims["email"]).to eq("passkey-taro@example.com")
      expect(claims["roles"]).to eq(["general"])

      tasks_stub = stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/tasks")
        .with(headers: { "Authorization" => "Bearer #{session.access_token}" })
        .to_return(status: 200, body: { tasks: [], total: 0 }.to_json)

      get "/api/tasks"
      expect(response).to have_http_status(:ok)
      expect(tasks_stub).to have_been_requested
    end

    it "未登録のcredentialでログインしようとすると401" do
      # 暗号的には正当なパスキー(fake_clientで生成)だが、backend側にレコードが
      # 無いケース(削除済み等)を模す。まず1回fake_client.createしないと、この
      # fake_client(=1つの仮想認証器)に紐づく資格情報が無くgetできない
      # (bff-railsのAPIは経由せず、fake_client自身の状態を準備するだけ)
      register_options = WebAuthn::Credential.options_for_create(user: { id: "999", name: "dummy" })
      fake_client.create(challenge: register_options.challenge)

      post "/api/auth/passkey/login/begin"
      login_begin_body = JSON.parse(response.body)
      get_result = fake_client.get(challenge: login_begin_body["options"]["challenge"])

      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/auth/webauthn/credentials/#{get_result['id']}")
        .to_return(status: 404)

      post "/api/auth/passkey/login/finish",
        params: { state: login_begin_body["state"], credential: get_result }

      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)["error"]).to eq("unknown_credential")
    end

    it "stateが不正/期限切れなら401" do
      post "/api/auth/passkey/login/finish", params: { state: "does-not-exist", credential: {} }
      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)["error"]).to eq("invalid_state")
    end

    # 【テスト監査で発見・修正】構文が壊れたJSONボディを送ると、Railsのparams解析が
    # 例外(ActionDispatch::Http::Parameters::ParseError)を送出し、rescue_fromが無く
    # 500になっていた(ApplicationControllerに追加したrescue_fromの回帰確認)
    it "構文が壊れたJSONボディを送ると400になる(500にならない)" do
      post "/api/auth/passkey/login/finish",
        params: "{not valid json truncated",
        headers: { "Content-Type" => "application/json" }

      expect(response).to have_http_status(:bad_request)
      expect(JSON.parse(response.body)["error"]).to eq("invalid_request")
    end

    # 【テスト監査で発見・修正】stateにString以外の型(数値)が来ても、
    # PasskeyChallengeStore.consumeがTypeErrorで落ちず、素直に「該当キー無し」
    # (=invalid_state)として扱われることを確認する
    it "stateがString以外の型(数値)でも500にならず、invalid_stateとして扱われる" do
      post "/api/auth/passkey/login/finish",
        params: { state: 12345, credential: {} }.to_json,
        headers: { "Content-Type" => "application/json" }

      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)["error"]).to eq("invalid_state")
    end
  end
end
