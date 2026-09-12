require "rails_helper"

# frontend-rails は bff を経由せず、Rails自身が OIDCクライアントとして Keycloak と Authorization Code フローを行う
# frontend-rails.oidc-gem(2値)で、omniauth-openid-connect(お任せ型)/openid_connect(手組み型)のどちらの実装を
# 使うかを切り替えられる
# OidcGemFlag.override!でテストごとに値を固定し、Feature Flag のポーリング(実ネットワーク)には依存しない
RSpec.describe "Sessions", type: :request do
  after do
    OidcGemFlag.override!(nil)
  end

  describe "未ログイン状態" do
    it "GET /welcome は /login へリダイレクトされる" do
      get "/welcome"
      expect(response).to redirect_to("/login")
    end

    it "GET /login はログイン画面を表示する" do
      get "/login"
      expect(response).to have_http_status(:ok)
      expect(response.body).to include("Keycloakでログイン")
    end
  end

  describe "omniauth-openid-connect(お任せ型)" do
    before do
      OidcGemFlag.override!("omniauth-openid-connect")
      OmniAuth.config.test_mode = true
    end

    after do
      OmniAuth.config.test_mode = false
      OmniAuth.config.mock_auth[:openid_connect] = nil
      Rails.application.env_config.delete("omniauth.auth")
    end

    describe "コールバック成功" do
      before do
        OmniAuth.config.mock_auth[:openid_connect] = OmniAuth::AuthHash.new(
          provider: "openid_connect",
          uid: "b8be7c29-a1f1-4208-bb16-f8798a976836",
          info: OmniAuth::AuthHash::InfoHash.new(name: "一般 ユーザー", email: "general-user@example.com"),
          credentials: OmniAuth::AuthHash.new(id_token: "dummy-id-token", token: "dummy-access-token"),
        )
        Rails.application.env_config["omniauth.auth"] = OmniAuth.config.mock_auth[:openid_connect]
        stub_backend_provision!
      end

      it "セッションが作られ、/welcome が「ようこそ、<name>さん」を表示する" do
        get "/auth/openid_connect/callback"
        expect(response).to redirect_to("/welcome")

        follow_redirect!
        expect(response).to have_http_status(:ok)
        expect(response.body).to include("ようこそ、一般 ユーザーさん")
        expect(response.body).to include("general-user@example.com")
        # 【CONTRACT.mdセクション22.9で追加】auth_modeがsessionへ保存され、welcome画面に表示される
        expect(response.body).to include("ログイン方式: keycloak")
      end

      # 【CONTRACT.mdセクション22.9で追加】パスキー機能追加に伴い、ログイン成功時に
      # backendへのJITプロビジョニング(BackendClient#provision!)を新たに呼ぶようになった。
      # ここが失敗した場合(backend障害・access_tokenのaud不一致等)にログインごと
      # 失敗させ、中途半端な(backend側にuser_idが無い)セッションを作らないことを確認する
      it "backendへのJITプロビジョニングが失敗した場合、ログインに失敗する" do
        stub_request(:post, "#{AppConfig.backend_rest_base_url}/internal/v1/users/provision")
          .to_return(status: 502, body: "bad gateway")

        get "/auth/openid_connect/callback"

        expect(response).to redirect_to("/login")
        follow_redirect!
        expect(response.body).to include("ログインに失敗しました")

        get "/welcome"
        expect(response).to redirect_to("/login")
      end

      # 【3回目のテスト監査(セキュリティ)で発見・修正】
      # ログイン成功時に reset_session を呼んでいなかったため、
      # Session Fixation(ログイン前のセッションの中身がログイン後もそのまま持ち越される)の余地があった
      #
      # 【テスト手法についての補足、2回試行錯誤した末にこの形にした】
      # 1回目: session.idの前後比較 → 既定のcookie_storeではidがreset_sessionと無関係に
      #   毎回変わって見えるため、無効化しても落ちない誤ったテストになった
      # 2回目: `session[:marker]=`をテストコードから直接書いてからリクエストする方法 →
      #   RSpecのrequest specでは実際のHTTPリクエスト/レスポンスを経由しない直接代入は
      #   次のリクエストのCookieに反映されず、これも無効化しても落ちなかった
      # 最終的に、reset_sessionというメソッドが実際に呼ばれたかどうかをmockで直接検証する
      # 方式にした(session storeの実装詳細に依存せず、最も確実に検証できる)
      it "ログイン成功時にreset_sessionが呼ばれる(Session Fixation対策)" do
        expect_any_instance_of(SessionsController).to receive(:reset_session).and_call_original

        get "/auth/openid_connect/callback"
      end
    end

    describe "ログアウト" do
      before do
        OmniAuth.config.mock_auth[:openid_connect] = OmniAuth::AuthHash.new(
          provider: "openid_connect",
          uid: "b8be7c29-a1f1-4208-bb16-f8798a976836",
          info: OmniAuth::AuthHash::InfoHash.new(name: "一般 ユーザー", email: "general-user@example.com"),
          credentials: OmniAuth::AuthHash.new(id_token: nil, token: "dummy-access-token"),
        )
        Rails.application.env_config["omniauth.auth"] = OmniAuth.config.mock_auth[:openid_connect]
        stub_backend_provision!
        get "/auth/openid_connect/callback"
      end

      it "セッションがクリアされ、再度 /welcome へアクセスすると /login へリダイレクトされる" do
        # id_tokenが無い(discoveryへ問い合わせずに済む)ケースではend_session_endpointを
        # 取得できずローカルセッションのクリアのみで完了する
        delete "/logout"
        expect(response).to redirect_to("/login")

        get "/welcome"
        expect(response).to redirect_to("/login")
      end
    end

    describe "認証失敗" do
      it "/auth/failure は /login へリダイレクトされエラーを表示する" do
        get "/auth/failure", params: { message: "invalid_credentials" }
        expect(response).to redirect_to("/login")
      end
    end
  end

  describe "openid_connect(手組み型)" do
    before do
      OidcGemFlag.override!("openid_connect")
    end

    it "POST /auth/openid_connect はKeycloakの認可エンドポイントへリダイレクトする(state/nonceをセッションに保存する)" do
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: "unused")

      post "/auth/openid_connect"

      expect(response).to have_http_status(:found)
      redirect_uri = URI.parse(response.headers["Location"])
      expect(redirect_uri.host).to eq("localhost")
      expect(redirect_uri.path).to eq("/realms/training/protocol/openid-connect/auth")
      query = Rack::Utils.parse_query(redirect_uri.query)
      expect(query["client_id"]).to eq("frontend-rails")
      expect(query["state"]).to be_present
      expect(query["nonce"]).to be_present
    end

    it "コールバックでcode/state/nonceを検証し、ID Tokenの署名検証まで通してセッションを作る" do
      # 1回目のリクエストでstate/nonceをセッションに保存させ、その値をコールバックにも使う
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: "will-be-overwritten")
      post "/auth/openid_connect"
      state = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["state"]
      nonce = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["nonce"]

      # nonceが実際にセッションへ保存された値と一致するように、ID Tokenを作り直してスタブする
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: nonce)
      stub_backend_provision!

      get "/auth/openid_connect/callback", params: { code: "dummy-auth-code", state: state }
      expect(response).to redirect_to("/welcome")

      follow_redirect!
      expect(response).to have_http_status(:ok)
      expect(response.body).to include("ようこそ、一般 ユーザーさん")
      expect(response.body).to include("general-user@example.com")
    end

    # 【3回目のテスト監査(セキュリティ)で発見・修正】omniauth-openid-connect版と同じく、
    # 手組み型のコールバック(handle_manual_callback)でもreset_sessionを呼んでいなかった
    # テスト手法はomniauth版と同じ理由(コメント参照)でmockによる呼び出し確認にしている
    it "ログイン成功時にreset_sessionが呼ばれる(Session Fixation対策)" do
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: "will-be-overwritten")
      post "/auth/openid_connect"
      state = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["state"]
      nonce = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["nonce"]

      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: nonce)
      stub_backend_provision!
      expect_any_instance_of(SessionsController).to receive(:reset_session).and_call_original
      get "/auth/openid_connect/callback", params: { code: "dummy-auth-code", state: state }
    end

    it "stateが不一致の場合はログインに失敗する" do
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: "nonce-1")
      post "/auth/openid_connect"

      get "/auth/openid_connect/callback", params: { code: "dummy-auth-code", state: "wrong-state" }
      expect(response).to redirect_to("/login")

      follow_redirect!
      expect(response.body).to include("ログインに失敗しました")
    end

    # 【テスト監査で追加】
    # state の一致チェックは SessionsController#handle_manual_callback 内で(token交換より前に)行われるのに対し、
    # nonce の一致チェックは ID Token検証(verify!)の中で行われる、という実装上の非対称がある
    # 既存テストは state 不一致しかカバーしておらず、「state は合っているが nonce だけ食い違う」という
    # 別のCSRF/リプレイ対策の抜け穴を検証できていなかった
    it "state一致だがID Token内のnonceが不一致の場合はログインに失敗する(リプレイ攻撃対策)" do
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: "attacker-supplied-nonce")
      post "/auth/openid_connect"
      state = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["state"]
      # セッションに保存された本来のnonceとは異なる値でID Tokenを署名させる
      # (=別のログイン試行で発行されたID Tokenを使い回そうとする攻撃を模擬)

      get "/auth/openid_connect/callback", params: { code: "dummy-auth-code", state: state }
      expect(response).to redirect_to("/login")

      follow_redirect!
      expect(response.body).to include("ログインに失敗しました")
    end

    it "token endpointがエラーを返す場合(認可コード失効・re-use等)はログインに失敗する" do
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: "will-be-overwritten")
      post "/auth/openid_connect"
      state = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["state"]

      # token endpointがinvalid_grant(認可コードが失効/再利用された想定)を返すよう上書きする
      stub_request(:post, "#{OidcTestHelpers::ISSUER}/protocol/openid-connect/token")
        .to_return(status: 400, body: { error: "invalid_grant" }.to_json, headers: { "Content-Type" => "application/json" })

      get "/auth/openid_connect/callback", params: { code: "dummy-auth-code", state: state }
      expect(response).to redirect_to("/login")

      follow_redirect!
      expect(response.body).to include("ログインに失敗しました")
    end

    it "JWKSに存在しないkidで署名されたID Token(改ざん・鍵ローテーション不整合)の場合はログインに失敗する" do
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: "will-be-overwritten")
      post "/auth/openid_connect"
      state = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["state"]
      nonce = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["nonce"]

      # JWKSには載っていない別の鍵で署名したID Tokenをtoken endpointから返す
      # (Keycloak側の鍵ローテーション直後の不整合、または署名の改ざんを模擬)
      forged_key = OpenSSL::PKey::RSA.generate(2048)
      claims = {
        iss: OidcTestHelpers::ISSUER, sub: "sub-1", aud: "frontend-rails",
        exp: Time.now.to_i + 300, iat: Time.now.to_i, nonce: nonce,
        name: "一般 ユーザー", email: "general-user@example.com",
      }
      forged_jwt = JSON::JWT.new(claims)
      forged_jwt.kid = "kid-not-in-jwks"
      forged_id_token = forged_jwt.sign(forged_key, :RS256).to_s
      stub_request(:post, "#{OidcTestHelpers::ISSUER}/protocol/openid-connect/token")
        .to_return(status: 200, body: { access_token: "dummy-access-token", token_type: "Bearer", id_token: forged_id_token }.to_json,
                    headers: { "Content-Type" => "application/json" })

      get "/auth/openid_connect/callback", params: { code: "dummy-auth-code", state: state }
      expect(response).to redirect_to("/login")

      follow_redirect!
      expect(response.body).to include("ログインに失敗しました")
    end

    it "ログアウトでセッションがクリアされ、再度 /welcome へアクセスすると /login へリダイレクトされる" do
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: "will-be-overwritten")
      post "/auth/openid_connect"
      state = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["state"]
      nonce = Rack::Utils.parse_query(URI.parse(response.headers["Location"]).query)["nonce"]
      stub_fake_keycloak!(sub: "sub-1", name: "一般 ユーザー", email: "general-user@example.com", nonce: nonce)
      stub_backend_provision!
      get "/auth/openid_connect/callback", params: { code: "dummy-auth-code", state: state }

      # ログアウト時にRP-Initiated Logout(end_session_endpoint)へリダイレクトを試みるため、
      # discoveryドキュメントのスタブが再度必要
      stub_request(:get, "#{OidcTestHelpers::ISSUER}/.well-known/openid-configuration")
        .to_return(status: 200, body: {
          issuer: OidcTestHelpers::ISSUER,
          authorization_endpoint: "#{OidcTestHelpers::ISSUER}/protocol/openid-connect/auth",
          token_endpoint: "#{OidcTestHelpers::ISSUER}/protocol/openid-connect/token",
          jwks_uri: "#{OidcTestHelpers::ISSUER}/protocol/openid-connect/certs",
          end_session_endpoint: "#{OidcTestHelpers::ISSUER}/protocol/openid-connect/logout",
          response_types_supported: ["code"],
          subject_types_supported: ["public"],
          id_token_signing_alg_values_supported: ["RS256"],
        }.to_json, headers: { "Content-Type" => "application/json" })

      delete "/logout"
      expect(response).to redirect_to(%r{\A#{OidcTestHelpers::ISSUER}/protocol/openid-connect/logout})

      get "/welcome"
      expect(response).to redirect_to("/login")
    end
  end
end
