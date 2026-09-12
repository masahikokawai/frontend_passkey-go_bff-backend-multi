require "faraday"
require "json"

# CONTRACT.mdセクション22.4: backendのパスキー内部API(/internal/v1/auth/webauthn/credentials/*)を
# 共有シークレットヘッダ(X-Webauthn-Internal-Token)で呼ぶ専用クライアント。
# パスキーでのログイン試行中はまだセッション/access tokenが無い(ログイン処理そのもの)ため、
# BackendClient(ユーザー自身のaccess tokenで呼ぶ)とは別に用意する
# (bffのGoが実装しているRequireLocalAuthInternalTokenと同じ設計、共有シークレットは
# bff(Go)・bff-railsで同じ値を設定する)
class WebauthnBackendClient
  class Error < StandardError; end

  # credential_idからuser_id・公開鍵・sign_countを引く。見つからなければnil
  def find_by_credential_id(credential_id)
    resp = conn.get("/internal/v1/auth/webauthn/credentials/#{credential_id}")
    return nil if resp.status == 404
    raise Error, "credential lookup failed: status=#{resp.status} body=#{resp.body}" unless resp.success?

    JSON.parse(resp.body)
  end

  # ログイン成功後、リプレイ攻撃対策のsign_countを更新する
  def update_sign_count!(credential_id, sign_count)
    resp = conn.patch("/internal/v1/auth/webauthn/credentials/#{credential_id}/sign-count") do |req|
      req.headers["Content-Type"] = "application/json"
      req.body = { sign_count: sign_count }.to_json
    end
    raise Error, "sign-count update failed: status=#{resp.status} body=#{resp.body}" unless resp.success?
  end

  private

  def conn
    @conn ||= Faraday.new(url: AppConfig.backend_rest_base_url) do |f|
      f.headers["X-Webauthn-Internal-Token"] = AppConfig.webauthn_internal_token
      f.options.timeout = 5
      f.options.open_timeout = 5
    end
  end
end
