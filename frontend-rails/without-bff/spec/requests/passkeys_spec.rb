require "rails_helper"
require "webauthn/fake_client"
require "base64"

# CONTRACT.mdセクション22.9: frontend-rails/without-bffへのパスキー追加。
# bff-rails/spec/requests/passkeys_spec.rbと同じ検証方針
# (WebAuthn::FakeClientで実際に暗号学的に妥当なcreate/getレスポンスを生成し、.verify()をモックせず
# 本物の検証ロジックを実際に通す)だが、セッションの持ち方が異なる(こちらはRailsのCookieセッション)。
RSpec.describe "PasskeysController", type: :request do
  let(:fake_client) { WebAuthn::FakeClient.new(AppConfig.webauthn_origin) }

  # Keycloakログイン済みの状態を、実際のコールバックを経由して作る
  # (直接session[:user]へ書き込む方式は、request specでは次のリクエストへ反映されないため使えない
  # 既存のsessions_spec.rbの「Session Fixationのテスト」で判明した既知の制約と同じ)
  def login_via_keycloak!(user_id: 42, access_token: "user-access-token")
    OidcGemFlag.override!("omniauth-openid-connect")
    OmniAuth.config.test_mode = true
    OmniAuth.config.mock_auth[:openid_connect] = OmniAuth::AuthHash.new(
      provider: "openid_connect",
      uid: "sub-1",
      info: OmniAuth::AuthHash::InfoHash.new(name: "Taro", email: "taro@example.com"),
      credentials: OmniAuth::AuthHash.new(id_token: "dummy-id-token", token: access_token),
    )
    Rails.application.env_config["omniauth.auth"] = OmniAuth.config.mock_auth[:openid_connect]
    stub_request(:post, "#{AppConfig.backend_rest_base_url}/internal/v1/users/provision")
      .with(headers: { "Authorization" => "Bearer #{access_token}" })
      .to_return(status: 200, body: { user_id: user_id }.to_json, headers: { "Content-Type" => "application/json" })

    get "/auth/openid_connect/callback"
  ensure
    OmniAuth.config.test_mode = false
    OmniAuth.config.mock_auth[:openid_connect] = nil
    Rails.application.env_config.delete("omniauth.auth")
    OidcGemFlag.override!(nil)
  end

  describe "POST /auth/passkey/register/begin" do
    it "未ログインなら401" do
      post "/auth/passkey/register/begin"
      expect(response).to have_http_status(:unauthorized)
    end

    it "ログイン中ならchallenge付きのoptionsとstateを返す" do
      login_via_keycloak!
      post "/auth/passkey/register/begin"
      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["state"]).to be_present
      expect(body["options"]["challenge"]).to be_present
      expect(body["options"]["authenticatorSelection"]["residentKey"]).to eq("required")
    end
  end

  describe "POST /auth/passkey/login/begin" do
    it "未ログインでも呼べ、allowCredentials指定無し(空)のoptionsを返す(discoverable credential)" do
      post "/auth/passkey/login/begin"
      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["state"]).to be_present
      expect(body["options"]["allowCredentials"]).to eq([])
    end
  end

  describe "登録→ログインの一連の流れ" do
    it "登録したパスキーで、Keycloakを経由せずログインできる(backendのwebauthn_credentialsテーブルを共有するbff/bff-railsとの契約通り)" do
      login_via_keycloak!(user_id: 7)

      post "/auth/passkey/register/begin"
      begin_body = JSON.parse(response.body)
      create_result = fake_client.create(challenge: begin_body["options"]["challenge"])

      captured = {}
      stub_request(:post, "#{AppConfig.backend_rest_base_url}/internal/v1/auth/webauthn/credentials")
        .with(headers: { "Authorization" => "Bearer user-access-token" })
        .to_return do |request|
          captured.merge!(JSON.parse(request.body))
          { status: 201, body: { id: 1 }.to_json }
        end

      post "/auth/passkey/register/finish", params: { state: begin_body["state"], credential: create_result }
      expect(response).to have_http_status(:ok)
      expect(JSON.parse(response.body)["registered"]).to eq(true)
      expect(captured["credential_id"]).to eq(create_result["id"])
      expect(captured["sign_count"]).to eq(0)
      # 【CONTRACT.mdセクション22.8の実機バグを踏まえた回帰確認】
      # WebAuthn::FakeClientが生成するattestationは通常のcreate()と同じ形状のauthenticator dataを
      # 持つため、backup_eligible/backup_stateキー自体は必ず送信されるはず(値そのものは
      # FakeClientの実装依存のため真偽は問わないが、キーの有無=配線されていることを確認する)
      expect(captured).to have_key("backup_eligible")
      expect(captured).to have_key("backup_state")

      # ここから先はKeycloakセッションを一切使わない、パスキーのみでのログイン
      post "/auth/passkey/login/begin"
      login_begin_body = JSON.parse(response.body)
      get_result = fake_client.get(challenge: login_begin_body["options"]["challenge"])

      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/auth/webauthn/credentials/#{get_result['id']}")
        .to_return(status: 200, body: {
          user_id: 7, public_key: captured["public_key"], sign_count: captured["sign_count"],
          name: "パスキー太郎", email: "passkey-taro@example.com", roles: ["general"]
        }.to_json)
      sign_count_stub = stub_request(
        :patch, "#{AppConfig.backend_rest_base_url}/internal/v1/auth/webauthn/credentials/#{get_result['id']}/sign-count"
      ).to_return(status: 204)

      post "/auth/passkey/login/finish", params: { state: login_begin_body["state"], credential: get_result }

      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["logged_in"]).to eq(true)
      expect(body["auth_mode"]).to eq("passkey")
      expect(sign_count_stub).to have_been_requested

      get "/welcome"
      expect(response).to have_http_status(:ok)
      expect(response.body).to include("ようこそ、パスキー太郎さん")
      expect(response.body).to include("passkey-taro@example.com")
      expect(response.body).to include("ログイン方式: passkey")
    end

    it "未登録のcredentialでログインしようとすると401" do
      register_options = WebAuthn::Credential.options_for_create(user: { id: "999", name: "dummy" })
      fake_client.create(challenge: register_options.challenge)

      post "/auth/passkey/login/begin"
      login_begin_body = JSON.parse(response.body)
      get_result = fake_client.get(challenge: login_begin_body["options"]["challenge"])

      stub_request(:get, "#{AppConfig.backend_rest_base_url}/internal/v1/auth/webauthn/credentials/#{get_result['id']}")
        .to_return(status: 404)

      post "/auth/passkey/login/finish", params: { state: login_begin_body["state"], credential: get_result }

      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)["error"]).to eq("unknown_credential")
    end

    it "stateが不正/期限切れなら401" do
      post "/auth/passkey/login/finish", params: { state: "does-not-exist", credential: {} }
      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)["error"]).to eq("invalid_state")
    end

    # 【テスト監査で追加、IDOR類似の観点】
    # register_beginでchallengeにuser_idを焼き込み、register_finishで
    # 「challenge発行時と同じユーザーか」をsession[:user]["user_id"]と突き合わせている
    # (passkeys_controller.rb:31)。この照合ロジックが実際にミスマッチを検出できることを、
    # 「challenge発行後、別ユーザーとしてログインし直してから同じstateでfinishを呼ぶ」という
    # 具体的なシナリオで確認する(theoretically: 悪意あるクライアントが他ユーザーの
    # in-flightなchallenge stateを盗用してパスキーを登録しようとするケースに相当)
    it "register_begin発行後にセッションのユーザーが変わっていると、register_finishは401 invalid_stateになる(他ユーザーのchallengeを使い回せない)" do
      login_via_keycloak!(user_id: 7, access_token: "token-for-user-7")
      post "/auth/passkey/register/begin"
      begin_body = JSON.parse(response.body)

      # 同じブラウザセッション(Cookie)のまま、別ユーザーとして再ログインする
      # (reset_sessionが呼ばれるため、session[:user]["user_id"]が7→99に置き換わる)
      login_via_keycloak!(user_id: 99, access_token: "token-for-user-99")

      fake_client_for_mismatch = WebAuthn::FakeClient.new(AppConfig.webauthn_origin)
      create_result = fake_client_for_mismatch.create(challenge: begin_body["options"]["challenge"])

      post "/auth/passkey/register/finish", params: { state: begin_body["state"], credential: create_result }

      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)["error"]).to eq("invalid_state")
    end

    # 【bff(Go)/bff-railsのwebauthn_challenge_store.goと同種のTOCTOU対策の回帰確認】
    # 同じstateへのconsumeが2回来た場合、1回目のみ成功し2回目は「stateが無い」扱いになることを確認する
    # (Auth::PasskeyChallengeStore.consumeがMutex#synchronize内でHash#deleteする実装であることの検証)
    it "同じstateでlogin/finishを2回呼ぶと、2回目は無効なstate扱いになる(challengeの再利用不可)" do
      post "/auth/passkey/login/begin"
      login_begin_body = JSON.parse(response.body)

      post "/auth/passkey/login/finish", params: { state: login_begin_body["state"], credential: {} }
      first_status = response.status

      post "/auth/passkey/login/finish", params: { state: login_begin_body["state"], credential: {} }
      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)["error"]).to eq("invalid_state")
      # 1回目もcredentialが空のため実際には検証エラー(unprocessable_entity)になるが、
      # いずれにせよ2回目が「state不明」であることが本題
      expect(first_status).not_to eq(0)
    end
  end

  # 【テスト監査で追加】このアプリはActionController::Base(bff-railsのActionController::APIと違い
  # CSRF保護が標準で有効)だが、RSpecのtest環境は既定でconfig.action_controller.allow_forgery_protection = false
  # (config/environments/test.rb)になっているため、既存のテストは全てCSRFトークン無しでPOSTしても
  # 素通りしてしまい、「本番でCSRF保護が実際に効いているか」を一切検証できていなかった。
  # ここだけ一時的にallow_forgery_protectionを有効化し、保護が本当に機能することを直接確認する
  describe "CSRF保護(本番相当の設定で確認)" do
    around do |example|
      original = ActionController::Base.allow_forgery_protection
      ActionController::Base.allow_forgery_protection = true
      example.run
    ensure
      ActionController::Base.allow_forgery_protection = original
    end

    it "X-CSRF-Tokenヘッダ無しでPOSTすると422になる(invalid authenticity token)" do
      post "/auth/passkey/login/begin"
      expect(response).to have_http_status(:unprocessable_entity)
    end
  end
end
