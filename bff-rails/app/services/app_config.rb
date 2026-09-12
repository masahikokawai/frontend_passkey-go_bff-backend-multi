# bff-railsの設定値。既存bff(Go)のconfig.goと同じ「環境変数を1箇所へ集約する」流儀を踏襲する
module AppConfig
  module_function

  def http_addr
    ENV.fetch("HTTP_ADDR", "8102")
  end

  def redis_url
    ENV.fetch("REDIS_URL", "redis://127.0.0.1:16379/0")
  end

  def keycloak_issuer
    ENV.fetch("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training")
  end

  def keycloak_client_id
    ENV.fetch("KEYCLOAK_CLIENT_ID", "bff-rails")
  end

  def keycloak_client_secret
    ENV.fetch("KEYCLOAK_CLIENT_SECRET", "bff-rails-local-dev-secret")
  end

  def redirect_uri
    ENV.fetch("REDIRECT_URI", "http://localhost:8102/auth/openid_connect/callback")
  end

  def post_logout_redirect_uri
    ENV.fetch("POST_LOGOUT_REDIRECT_URI", "http://localhost:5175/")
  end

  def frontend_origin
    ENV.fetch("FRONTEND_ORIGIN", "http://localhost:5175")
  end

  def backend_rest_base_url
    ENV.fetch("BACKEND_REST_BASE_URL", "http://localhost:8090")
  end

  def feature_flag_export_url
    ENV.fetch("FEATURE_FLAG_EXPORT_URL", "#{backend_rest_base_url}/internal/v1/feature-flags/export")
  end

  def feature_flag_poll_token
    ENV.fetch("FEATURE_FLAG_POLL_TOKEN", "local-dev-feature-flag-poll-token")
  end

  def feature_flag_poll_interval_seconds
    Integer(ENV.fetch("FEATURE_FLAG_POLL_INTERVAL_SECONDS", "10"))
  end

  # CONTRACT.mdセクション22: パスキー(WebAuthn)関連設定
  # rp_idはドメインのみ(ポート無し)。bff(Go)側と同じ"localhost"にすることで、
  # 同一ブラウザ上でbff/bff-rails両方から同じパスキーが選択可能になる
  # (RP IDは資格情報のスコープを決める値で、実際に検証されるoriginとは別物)
  def webauthn_rp_id
    ENV.fetch("WEBAUTHN_RP_ID", "localhost")
  end

  def webauthn_rp_name
    ENV.fetch("WEBAUTHN_RP_NAME", "bff-gin (bff-rails)")
  end

  # originはこのbff-rails自身の想定呼び出し元、つまりfrontend-rails/with-bffのオリジン
  def webauthn_origin
    ENV.fetch("WEBAUTHN_ORIGIN", "http://localhost:5175")
  end

  def webauthn_internal_token
    ENV.fetch("WEBAUTHN_INTERNAL_TOKEN", "local-dev-webauthn-internal-token")
  end

  # パスキーのみでログインしたセッション用に、bff(Go)のinternal/auth/local_jwt.goと
  # 同じ共有シークレットで自前JWT(iss=bff-gin-local-hmac)を発行するために使う。
  # backend側のauthjwt.HMACVerifierは「この値で正しく署名されているか」しか見ておらず、
  # 発行元がbffかbff-railsかを区別しないため、bff側と全く同じ値を設定するだけで
  # backend側のコード変更なしにこの仕組みへ相乗りできる
  def local_auth_hmac_secret
    ENV.fetch("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me")
  end
end
