# backend/internal/authjwt/middleware.go(REST v1向けRequireAuth)のRails版
#
# Authorizationヘッダが無い/Bearer形式でない場合は401 unauthorized、
# JWT検証自体が失敗した場合は401 invalid_token、検証は通ったがusersテーブルに
# 該当行が無い場合は403 user_not_provisioned、という3段階のエラー区別をGoと合わせる
class ApplicationController < ActionController::API
  rescue_from ActiveRecord::RecordNotFound do
    render json: { error: "not_found" }, status: :not_found
  end

  private

  def authenticate!
    header = request.headers["Authorization"].to_s
    token = header.start_with?("Bearer ") ? header.delete_prefix("Bearer ") : nil
    if token.blank?
      render json: { error: "unauthorized" }, status: :unauthorized
      return false
    end

    claims =
      begin
        jwt_verifier.verify(token)
      rescue JwtVerifier::VerificationError
        render json: { error: "invalid_token" }, status: :unauthorized
        return false
      end

    @current_user =
      begin
        UserResolver.resolve(claims)
      rescue UserResolver::UserNotProvisioned
        render json: { error: "user_not_provisioned" }, status: :forbidden
        return false
      end

    true
  end

  def current_user
    @current_user
  end

  def jwt_verifier
    @jwt_verifier ||= JwtVerifier.new
  end

  # backend/internal/handler/v1/render.go の renderServiceError 相当
  # ActiveRecord::RecordInvalidをTaskモデルのバリデーションエラーから422へ変換する
  def render_validation_error(record)
    render json: { error: "validation_error", message: record.errors.full_messages.join(", ") }, status: :unprocessable_entity
  end
end
