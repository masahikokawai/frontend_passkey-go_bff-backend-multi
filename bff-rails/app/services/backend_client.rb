require "faraday"

# backendの内部API(/internal/v1/users/provision・/internal/v1/tasks)を、
# ログイン中ユーザー自身のaccess token(aud=backend付き)で直接呼ぶ
# (bffがtask_client_v1.go等で行っているのと同じ「検証済みトークンからbackend自身が
# user_idを導出する」設計を踏襲する。CONTRACT.mdの議論参照)
class BackendClient
  class Error < StandardError; end

  # 【2回目のテスト監査で追加】backendがaccess_token期限切れ等で401/403を返したケースを、
  # それ以外のbackendエラー(5xx・不正なレスポンス等)と区別できるようにする。
  # 呼び出し側(TasksController)はこの例外だけを捕捉してtoken refreshを試みる
  # (それ以外のErrorは単純に502として扱う、リトライしても直らないため)
  class UnauthorizedError < Error; end

  def initialize(access_token)
    @access_token = access_token
  end

  # JITプロビジョニング。リクエストボディ不要、Authorizationヘッダのみでよい
  def provision!
    resp = conn.post("/internal/v1/users/provision")
    raise_for_status!(resp, "provision failed")
    JSON.parse(resp.body)
  end

  def list_tasks
    resp = conn.get("/internal/v1/tasks")
    raise_for_status!(resp, "list tasks failed")
    JSON.parse(resp.body)
  end

  # CONTRACT.mdセクション22.4: ログイン中ユーザーが新しいパスキーを登録する。
  # 認証は今のアクセストークン(aud=backend付き)をそのまま使うため、共有シークレット方式の
  # WebauthnBackendClientとは別(こちらはBackendClient、ユーザー自身のトークンで呼ぶ)
  def register_passkey!(credential_id:, public_key:, sign_count:, transports: [], name: nil)
    resp = conn.post("/internal/v1/auth/webauthn/credentials") do |req|
      req.headers["Content-Type"] = "application/json"
      req.body = {
        credential_id: credential_id,
        public_key: public_key,
        sign_count: sign_count,
        transports: transports,
        name: name
      }.compact.to_json
    end
    raise Error, "passkey registration failed: status=#{resp.status} body=#{resp.body}" unless resp.success?

    JSON.parse(resp.body)
  end

  private

  def raise_for_status!(resp, message)
    return if resp.success?

    if [401, 403].include?(resp.status)
      raise UnauthorizedError, "#{message}: status=#{resp.status} body=#{resp.body}"
    end

    raise Error, "#{message}: status=#{resp.status} body=#{resp.body}"
  end

  def conn
    @conn ||= Faraday.new(url: AppConfig.backend_rest_base_url) do |f|
      f.headers["Authorization"] = "Bearer #{@access_token}"
      f.options.timeout = 5
      f.options.open_timeout = 5
    end
  end
end
