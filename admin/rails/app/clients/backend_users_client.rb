require "net/http"
require "json"

# BackendUsersClient は backend(training-go/bff-gin/backend)の
# /internal/v1/admin/users 系APIを叩くだけのシンプルなHTTPクライアント
#
# CONTRACT.mdセクション17.1: Feature Flag(admin/rails→MySQL直結)とは異なり、
# ユーザー管理は「最後の管理者を降格・削除できない」という業務ルールを
# backend側(service/user.go)の1箇所にだけ実装したいため、admin/railsからは
# ActiveRecordでusersテーブルへ直接書き込まず、必ずこのクライアント経由でbackendへ委譲する
class BackendUsersClient
  class Error < StandardError; end
  # backendが422 かつ error="validation_error" を返した場合(email重複・password空・role不正など)
  class ValidationError < Error; end
  # backendが422 かつ error="last_manager_user" を返した場合(最後の管理者を降格・削除しようとした)
  # 【実装時に判明】backendのrenderServiceErrorはErrLastManagerUserもErrValidationと
  # 同じ422を返す設計(local-auth実装のinvalid_credentials/password_expiredと同じく、
  # ステータスコードではなくJSONボディの`error`キーで種別を判別する既存パターンに統一)
  # 当初409を前提に実装していたが、backend側の実装完了後にこの食い違いが判明し修正した
  class LastManagerError < Error; end
  # backendが404を返した場合(対象ユーザーが存在しない)
  class NotFoundError < Error; end

  def initialize(
    base_url: ENV.fetch("BACKEND_INTERNAL_BASE_URL", "http://localhost:8090"),
    token: ENV.fetch("ADMIN_INTERNAL_TOKEN", "local-dev-admin-internal-token")
  )
    @base_url = base_url
    @token = token
  end

  def list
    body = request(:get, "/internal/v1/admin/users")
    body[:users] || []
  end

  def create(name:, email:, password:, role:)
    request(:post, "/internal/v1/admin/users", { name: name, email: email, password: password, role: role })
  end

  def update_role(id:, role:)
    request(:patch, "/internal/v1/admin/users/#{id}/role", { role: role })
  end

  def delete(id:)
    request(:delete, "/internal/v1/admin/users/#{id}")
  end

  private

  REQUEST_CLASSES = {
    get: Net::HTTP::Get,
    post: Net::HTTP::Post,
    patch: Net::HTTP::Patch,
    delete: Net::HTTP::Delete
  }.freeze

  def request(method, path, body = nil)
    uri = URI.join(@base_url, path)
    req = REQUEST_CLASSES.fetch(method).new(uri)
    req["X-Admin-Internal-Token"] = @token
    if body
      req["Content-Type"] = "application/json"
      req.body = body.to_json
    end

    res = Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == "https") { |http| http.request(req) }
    handle_response(res)
  end

  def handle_response(res)
    case res.code.to_i
    when 200, 201
      JSON.parse(res.body || "{}", symbolize_names: true)
    when 204
      {}
    when 404
      raise NotFoundError, error_message(res) || "対象のユーザーが見つかりません"
    when 422
      if error_key(res) == "last_manager_user"
        raise LastManagerError, error_message(res) || "最後の管理者は変更・削除できません"
      end
      raise ValidationError, error_message(res) || "入力内容が不正です"
    else
      raise Error, "backend呼び出しに失敗しました(status=#{res.code})"
    end
  end

  # error_key はbackendのエラー種別判別キー(例: "last_manager_user"/"validation_error")
  def error_key(res)
    JSON.parse(res.body, symbolize_names: true)[:error]
  rescue JSON::ParserError, TypeError
    nil
  end

  # error_message は画面へ表示する人間向けの文言(常に付与されるわけではない)
  def error_message(res)
    JSON.parse(res.body, symbolize_names: true)[:message]
  rescue JSON::ParserError, TypeError
    nil
  end
end
