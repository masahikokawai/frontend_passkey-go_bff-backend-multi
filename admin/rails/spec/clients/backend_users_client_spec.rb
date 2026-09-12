require "rails_helper"

RSpec.describe BackendUsersClient do
  subject(:client) { described_class.new(base_url: "http://backend.example", token: "test-token") }

  # Net::HTTP.start(...) { |http| http.request(req) } という形の呼び出しを、実際にネットワークへ出さずに検証するためのヘルパー
  # ブロックの戻り値がNet::HTTP.startの戻り値になる、というRubyの仕様に合わせて
  # スタブ自体もブロックを実行してその結果を返すようにする(WebMock等の追加gemなしで検証するため)
  def stub_http(code:, body: nil)
    response = instance_double(Net::HTTPResponse, code: code.to_s, body: body)
    http_double = instance_double(Net::HTTP)
    allow(http_double).to receive(:request) do |req|
      @captured_request = req
      response
    end
    allow(Net::HTTP).to receive(:start) do |*_args, **_kwargs, &blk|
      blk.call(http_double)
    end
    response
  end

  describe "#list" do
    it "GET /internal/v1/admin/users を叩き、users配列を返す" do
      stub_http(code: 200, body: { users: [{ id: 1, name: "A", email: "a@example.com", role: "general" }] }.to_json)

      expect(client.list).to eq([{ id: 1, name: "A", email: "a@example.com", role: "general" }])
      expect(@captured_request.path).to eq("/internal/v1/admin/users")
      expect(@captured_request["X-Admin-Internal-Token"]).to eq("test-token")
    end

    it "usersキーが無い(空)場合は空配列を返す" do
      stub_http(code: 200, body: {}.to_json)
      expect(client.list).to eq([])
    end
  end

  describe "#create" do
    it "POSTでname/email/password/roleを送り、作成結果を返す" do
      stub_http(code: 201, body: { user_id: 5, name: "B", email: "b@example.com", role: "general" }.to_json)

      result = client.create(name: "B", email: "b@example.com", password: "password", role: "general")

      expect(result).to eq({ user_id: 5, name: "B", email: "b@example.com", role: "general" })
      expect(@captured_request.body).to eq({ name: "B", email: "b@example.com", password: "password", role: "general" }.to_json)
    end

    it "backendが422を返した場合、ValidationErrorを送出する" do
      stub_http(code: 422, body: { error: "validation_error", message: "email is already taken" }.to_json)

      expect { client.create(name: "B", email: "dup@example.com", password: "x", role: "general") }
        .to raise_error(BackendUsersClient::ValidationError, "email is already taken")
    end
  end

  describe "#update_role" do
    it "PATCHでroleを送る" do
      stub_http(code: 204)
      client.update_role(id: 1, role: "management")
      expect(@captured_request.path).to eq("/internal/v1/admin/users/1/role")
    end

    it "backendが422 かつ error=last_manager_user を返した場合、LastManagerErrorを送出する(最後の管理者ガード)" do
      stub_http(code: 422, body: { error: "last_manager_user", message: "cannot demote the last management user" }.to_json)

      expect { client.update_role(id: 1, role: "general") }
        .to raise_error(BackendUsersClient::LastManagerError, "cannot demote the last management user")
    end
  end

  describe "#delete" do
    it "DELETEを送る" do
      stub_http(code: 204)
      client.delete(id: 1)
      expect(@captured_request.path).to eq("/internal/v1/admin/users/1")
    end

    it "backendが404を返した場合、NotFoundErrorを送出する" do
      stub_http(code: 404, body: { error: "not_found", message: "not found" }.to_json)
      expect { client.delete(id: 999) }.to raise_error(BackendUsersClient::NotFoundError, "not found")
    end

    it "backendが422 かつ error=last_manager_user を返した場合、LastManagerErrorを送出する" do
      stub_http(code: 422, body: { error: "last_manager_user", message: "cannot delete the last management user" }.to_json)
      expect { client.delete(id: 1) }.to raise_error(BackendUsersClient::LastManagerError)
    end
  end
end
