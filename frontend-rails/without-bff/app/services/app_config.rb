# CONTRACT.mdセクション22.9: パスキー機能の追加で、このアプリが初めてbackendへ接続するようになった。
# 既存コード(ManualOidcClient等)は素のENV.fetchを直接使っているが、
# bff-rails(app/services/app_config.rb)と同じ「設定値を1箇所へ集約する」流儀を、
# 新規に追加するbackend接続関連の値についてのみ踏襲する
# (既存のOIDC関連ENV.fetch呼び出しは、今回のスコープ外のため変更しない)
module AppConfig
  module_function

  def backend_rest_base_url
    ENV.fetch("BACKEND_REST_BASE_URL", "http://localhost:8090")
  end

  # rp_idはドメインのみ(ポート無し)。bff(Go)・bff-rails側と同じ"localhost"にすることで、
  # 同一ブラウザ上でどのアプリから登録したパスキーも同じ資格情報として選択可能になる
  def webauthn_rp_id
    ENV.fetch("WEBAUTHN_RP_ID", "localhost")
  end

  def webauthn_rp_name
    ENV.fetch("WEBAUTHN_RP_NAME", "bff-gin (frontend-rails/without-bff)")
  end

  def webauthn_origin
    ENV.fetch("WEBAUTHN_ORIGIN", "http://localhost:5174")
  end

  def webauthn_internal_token
    ENV.fetch("WEBAUTHN_INTERNAL_TOKEN", "local-dev-webauthn-internal-token")
  end

  # パスキーのみでログインしたセッション用に、bff(Go)/bff-railsと同じ共有シークレットで
  # 自前JWT(iss=bff-gin-local-hmac)を発行するために使う。backend側のauthjwt.HMACVerifierは
  # 「この値で正しく署名されているか」しか見ておらず発行元を区別しないため、bff/bff-railsと
  # 全く同じ値を設定するだけでbackend側のコード変更なしにこの仕組みへ相乗りできる
  def local_auth_hmac_secret
    ENV.fetch("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me")
  end
end
