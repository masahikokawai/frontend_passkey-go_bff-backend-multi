require "rails_helper"

RSpec.describe "MeController", type: :request do
  describe "GET /api/me" do
    it "未ログインなら401" do
      get "/api/me"
      expect(response).to have_http_status(:unauthorized)
    end

    it "ログイン中ならname/email/auth_modeを返す" do
      session = SessionStore::Session.new(
        keycloak_sub: "s", name: "Taro", email: "taro@example.com", access_token: "at", refresh_token: "r",
        auth_mode: "keycloak"
      )
      id = SessionStore.create(session)
      cookies["bff_rails_session"] = id

      get "/api/me"

      expect(response).to have_http_status(:ok)
      expect(JSON.parse(response.body)).to eq(
        { "name" => "Taro", "email" => "taro@example.com", "auth_mode" => "keycloak" }
      )
    end
  end
end
