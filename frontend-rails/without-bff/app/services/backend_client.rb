require "faraday"
require "json"

# CONTRACT.mdセクション22.9: backendの内部API(/internal/v1/users/provision・
# /internal/v1/auth/webauthn/credentials)を、ログイン中ユーザー自身のaccess token
# (aud=backend付き、21.2でKeycloakクライアントにoidc-audience-mapperを追加済み)で直接呼ぶ
# (bff-rails/app/services/backend_client.rbと同じ設計。ただしこのアプリはTask一覧を扱わない
# スコープのため、provision!とregister_passkey!のみを持つ)
class BackendClient
  class Error < StandardError; end

  def initialize(access_token)
    @access_token = access_token
  end

  # JITプロビジョニング。リクエストボディ不要、Authorizationヘッダのみでよい
  def provision!
    resp = conn.post("/internal/v1/users/provision")
    raise_for_status!(resp, "provision failed")
    JSON.parse(resp.body)
  end

  # CONTRACT.mdセクション22.4: ログイン中ユーザーが新しいパスキーを登録する。
  # 認証は今のaccess_token(aud=backend付き)をそのまま使うため、共有シークレット方式の
  # WebauthnBackendClientとは別(こちらはBackendClient、ユーザー自身のトークンで呼ぶ)
  def register_passkey!(credential_id:, public_key:, sign_count:, backup_eligible: false, backup_state: false, transports: [], name: nil)
    resp = conn.post("/internal/v1/auth/webauthn/credentials") do |req|
      req.headers["Content-Type"] = "application/json"
      req.body = {
        credential_id: credential_id,
        public_key: public_key,
        sign_count: sign_count,
        backup_eligible: backup_eligible,
        backup_state: backup_state,
        transports: transports,
        name: name
      }.compact.to_json
    end
    raise_for_status!(resp, "passkey registration failed")
    JSON.parse(resp.body)
  end

  private

  def raise_for_status!(resp, message)
    raise Error, "#{message}: status=#{resp.status} body=#{resp.body}" unless resp.success?
  end

  def conn
    @conn ||= Faraday.new(url: AppConfig.backend_rest_base_url) do |f|
      f.headers["Authorization"] = "Bearer #{@access_token}"
      f.options.timeout = 5
      f.options.open_timeout = 5
    end
  end
end
