require "rails_helper"

# CONTRACT.mdセクション11(BFF非経由の外部公開API)のend-to-end確認。
# backend(Go)側のhandler/external/task_test.go相当。
RSpec.describe "External::V1::Tasks", type: :request do
  let(:hmac_secret) { "test-hmac-secret" }
  let!(:user) { User.create!(keycloak_sub: "sub-1", email: "u@example.com", name: "U", role: 1) }

  before do
    allow(JwtVerifier).to receive(:new).and_return(
      JwtVerifier.new(local_hmac_secret: hmac_secret, expected_audience: "backend")
    )
    ExternalPaginationFlag.reset!
  end

  after { ExternalPaginationFlag.reset! }

  # 外部公開APIはトークンの発行元(Keycloak/ローカル)を区別しないため、
  # テストの単純化のためローカルHMAC署名のトークンにazpクレームを足して使う
  # (JwtVerifier自体はazpの意味を知らず、ただpayloadから読んで返すだけ)
  def bearer(azp: "external-api-client")
    token = JWT.encode(
      { sub: "999", iss: JwtVerifier::LOCAL_HMAC_ISSUER, aud: "backend", azp: azp, exp: 1.hour.from_now.to_i },
      hmac_secret, "HS256"
    )
    { "Authorization" => "Bearer #{token}" }
  end

  describe "認証" do
    it "Authorizationヘッダが無ければ401 unauthorized" do
      get "/external/v1/tasks", params: { user_id: user.id }
      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)).to eq("error" => "unauthorized")
    end

    it "トークンの検証に失敗すれば401 invalid_token" do
      get "/external/v1/tasks", params: { user_id: user.id }, headers: { "Authorization" => "Bearer garbage" }
      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)).to eq("error" => "invalid_token")
    end

    it "azpがexternal-api-clientと一致しなければ403 client_not_allowed" do
      get "/external/v1/tasks", params: { user_id: user.id }, headers: bearer(azp: "someone-else")
      expect(response).to have_http_status(:forbidden)
      expect(JSON.parse(response.body)).to eq("error" => "client_not_allowed")
    end
  end

  describe "user_idバリデーション" do
    it "user_idが無ければ400" do
      get "/external/v1/tasks", headers: bearer
      expect(response).to have_http_status(:bad_request)
      expect(JSON.parse(response.body)).to eq("error" => "user_id is required")
    end

    it "user_idが数値でなければ400" do
      get "/external/v1/tasks", params: { user_id: "abc" }, headers: bearer
      expect(response).to have_http_status(:bad_request)
      expect(JSON.parse(response.body)).to eq("error" => "invalid user_id")
    end
  end

  describe "flag OFF(既定): offsetページング" do
    before { FeatureFlag.create!(flag_key: "backend.external-tasks-pagination-v2", default_variation: "off", enabled: true) }

    it "page/page_size/totalを含む形状で返す。user_idはレスポンスに含めない" do
      3.times { |i| user.tasks.create!(name: "t#{i}", status: "waiting", finished_on: Date.today) }

      get "/external/v1/tasks", params: { user_id: user.id, page: 1, page_size: 2 }, headers: bearer

      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["page"]).to eq(1)
      expect(body["page_size"]).to eq(2)
      expect(body["total"]).to eq(3)
      expect(body["tasks"].length).to eq(2)
      expect(body["tasks"].first).not_to have_key("user_id")
    end

    # 【テストカバレッジ監査で追記】タスク0件の境界値テストが無かった(Rust/Scala実装との
    # 非対称性を埋める)。空配列・total=0を返すだけでエラーにならないことを確認する。
    it "タスクが0件でも200でtasks=[]・total=0を返す" do
      get "/external/v1/tasks", params: { user_id: user.id, page: 1, page_size: 10 }, headers: bearer

      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["tasks"]).to eq([])
      expect(body["total"]).to eq(0)
    end
  end

  describe "flag ON: keyset(cursor)ページング" do
    before { FeatureFlag.create!(flag_key: "backend.external-tasks-pagination-v2", default_variation: "on", enabled: true) }

    it "next_cursorを含む形状で返し、次ページを辿ると残りのタスクが取得できる" do
      3.times { |i| user.tasks.create!(name: "t#{i}", status: "waiting", finished_on: Date.today + i) }

      get "/external/v1/tasks", params: { user_id: user.id, limit: 2 }, headers: bearer
      expect(response).to have_http_status(:ok)
      first_page = JSON.parse(response.body)
      expect(first_page["tasks"].length).to eq(2)
      expect(first_page["next_cursor"]).not_to be_nil

      get "/external/v1/tasks", params: { user_id: user.id, limit: 2, cursor: first_page["next_cursor"] }, headers: bearer
      expect(response).to have_http_status(:ok)
      second_page = JSON.parse(response.body)
      expect(second_page["tasks"].length).to eq(1)
      expect(second_page["next_cursor"]).to be_nil
    end

    it "不正なcursorは422" do
      get "/external/v1/tasks", params: { user_id: user.id, cursor: "not-valid-base64!!" }, headers: bearer
      expect(response).to have_http_status(:unprocessable_entity)
    end

    # 【テストカバレッジ監査で追記】cursor版でもタスク0件の境界値テストが無かった
    it "タスクが0件でも200でtasks=[]・next_cursor=nullを返す" do
      get "/external/v1/tasks", params: { user_id: user.id, limit: 10 }, headers: bearer

      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["tasks"]).to eq([])
      expect(body["next_cursor"]).to be_nil
    end
  end
end
