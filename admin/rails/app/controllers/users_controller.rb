# UsersController は Go+Gin実装(admin/go)と同じ機能を提供するRails版
# CONTRACT.mdセクション17:
# backendの /internal/v1/admin/users をHTTP経由で叩くだけで、usersテーブルへは直接触れない
# (理由は BackendUsersClient のコメント参照)
class UsersController < ApplicationController
  def index
    @users = client.list
  end

  def create
    client.create(
      name: params[:name],
      email: params[:email],
      password: params[:password],
      role: params[:role]
    )
    redirect_to users_path, flash: { success: "ユーザーを作成しました" }
  rescue BackendUsersClient::Error => e
    redirect_to users_path, flash: { danger: e.message }
  end

  def update_role
    client.update_role(id: params[:id], role: params[:role])
    redirect_to users_path, flash: { success: "roleを更新しました" }
  rescue BackendUsersClient::Error => e
    redirect_to users_path, flash: { danger: e.message }
  end

  def destroy
    client.delete(id: params[:id])
    redirect_to users_path, flash: { success: "ユーザーを削除しました" }
  rescue BackendUsersClient::Error => e
    redirect_to users_path, flash: { danger: e.message }
  end

  private

  def client
    @client ||= BackendUsersClient.new
  end
end
