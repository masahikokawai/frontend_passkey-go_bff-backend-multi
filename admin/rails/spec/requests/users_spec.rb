require "rails_helper"

RSpec.describe "Users", type: :request do
  # UsersControllerはBackendUsersClient経由でbackendへ委譲するだけなので、
  # ここではクライアントをモックしてコントローラ自身のロジック(エラーハンドリング・リダイレクト・flashメッセージ)だけを検証する
  # backendとの実際の疎通はコーディネーター側でbackend実装完了後に確認する
  let(:client) { instance_double(BackendUsersClient) }

  before do
    allow(BackendUsersClient).to receive(:new).and_return(client)
    # follow_redirect!でindexへ遷移するテストのための既定スタブ(各テストで上書き可能)
    allow(client).to receive(:list).and_return([])
  end

  describe "GET /users" do
    it "一覧が表示される" do
      allow(client).to receive(:list).and_return([{ id: 1, name: "A", email: "a@example.com", role: "general" }])

      get users_path

      expect(response).to have_http_status(:ok)
      expect(response.body).to include("a@example.com")
    end
  end

  describe "GET /users のパスキー列(CONTRACT.mdセクション22.7)" do
    it "has_passkey=trueのユーザーは「登録済み」、falseのユーザーは「未登録」と表示される" do
      allow(client).to receive(:list).and_return([
        { id: 1, name: "登録済み太郎", email: "with-passkey@example.com", role: "general", has_passkey: true },
        { id: 2, name: "未登録花子", email: "without-passkey@example.com", role: "general", has_passkey: false }
      ])

      get users_path

      expect(response).to have_http_status(:ok)
      expect(response.body).to include("登録済み")
      expect(response.body).to include("未登録")
    end

    it "backendがhas_passkeyを返さない(古いレスポンス)場合でもエラーにならず「未登録」扱いになる" do
      allow(client).to receive(:list).and_return([{ id: 1, name: "A", email: "a@example.com", role: "general" }])

      get users_path

      expect(response).to have_http_status(:ok)
      expect(response.body).to include("未登録")
    end
  end

  describe "POST /users" do
    it "作成に成功すると一覧へリダイレクトし、成功メッセージが出る" do
      allow(client).to receive(:create).with(name: "B", email: "b@example.com", password: "password", role: "general")
        .and_return({ user_id: 2, name: "B", email: "b@example.com", role: "general" })

      post users_path, params: { name: "B", email: "b@example.com", password: "password", role: "general" }

      expect(response).to redirect_to(users_path)
      follow_redirect!
      expect(response.body).to include("ユーザーを作成しました")
    end

    it "backendがバリデーションエラー(422)を返すと、一覧へリダイレクトしエラーメッセージが出る" do
      allow(client).to receive(:create).and_raise(BackendUsersClient::ValidationError, "email is already taken")

      post users_path, params: { name: "B", email: "dup@example.com", password: "password", role: "general" }

      expect(response).to redirect_to(users_path)
      follow_redirect!
      expect(response.body).to include("email is already taken")
    end
  end

  describe "PATCH /users/:id/update_role" do
    it "roleを変更できる" do
      allow(client).to receive(:update_role).with(id: "1", role: "management").and_return({})

      patch update_role_user_path(1), params: { role: "management" }

      expect(response).to redirect_to(users_path)
      follow_redirect!
      expect(response.body).to include("roleを更新しました")
    end

    it "最後の管理者を降格しようとすると(422)、エラーメッセージが出る" do
      allow(client).to receive(:update_role).and_raise(BackendUsersClient::LastManagerError, "cannot demote the last management user")

      patch update_role_user_path(1), params: { role: "general" }

      expect(response).to redirect_to(users_path)
      follow_redirect!
      expect(response.body).to include("cannot demote the last management user")
    end
  end

  describe "DELETE /users/:id" do
    it "削除できる" do
      allow(client).to receive(:delete).with(id: "1").and_return({})

      delete user_path(1)

      expect(response).to redirect_to(users_path)
      follow_redirect!
      expect(response.body).to include("ユーザーを削除しました")
    end

    it "最後の管理者を削除しようとすると(422)、エラーメッセージが出る" do
      allow(client).to receive(:delete).and_raise(BackendUsersClient::LastManagerError, "cannot delete the last management user")

      delete user_path(1)

      expect(response).to redirect_to(users_path)
      follow_redirect!
      expect(response.body).to include("cannot delete the last management user")
    end

    it "存在しないユーザーを削除しようとすると(404)、エラーメッセージが出る" do
      allow(client).to receive(:delete).and_raise(BackendUsersClient::NotFoundError, "not found")

      delete user_path(999)

      expect(response).to redirect_to(users_path)
      follow_redirect!
      expect(response.body).to include("not found")
    end
  end
end
