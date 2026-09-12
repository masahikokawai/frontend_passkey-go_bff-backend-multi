class MeController < ApplicationController
  # GET /api/me — frontend-rails/with-bffがログイン状態を確認するための素朴なエンドポイント
  def show
    if current_session
      render json: { name: current_session.name, email: current_session.email, auth_mode: current_session.auth_mode }
    else
      render json: { error: "unauthorized" }, status: :unauthorized
    end
  end
end
