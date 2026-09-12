require "jwt"

module Auth
  # パスキーのみでログインしたセッションには、backend呼び出しに使えるOIDCの
  # access_tokenが存在しない(パスキーはKeycloakを経由しないため)。
  #
  # これを解決するため、bff(Go)のinternal/auth/local_jwt.go(ローカルHMAC認証、
  # CONTRACT.mdセクション16.4)・bff-rails/app/services/auth/local_backend_token_issuer.rbと
  # 全く同じクレーム形状・署名方式(HS256、共有シークレット)で自前のJWTを発行し、それを
  # そのままbackendへ転送するaccess_token代わりに使う。backend側のauthjwt.HMACVerifierは
  # 「iss=bff-gin-local-hmacの署名が正しいか」だけを見ており、発行元を区別しないため、
  # 同じ共有シークレットを設定するだけでbackend側のコード変更は一切不要になる
  module LocalBackendTokenIssuer
    ISSUER = "bff-gin-local-hmac".freeze
    AUDIENCE = "backend".freeze
    TTL_SECONDS = 24 * 60 * 60

    module_function

    # user_id: backend内部のユーザーID(文字列化してsubクレームに入れる)
    # name/email/roles: セッション表示・role判定に使う値(rolesは配列)
    def issue(user_id:, name:, email:, roles:)
      now = Time.now.to_i
      payload = {
        iss: ISSUER,
        sub: user_id.to_s,
        aud: [AUDIENCE],
        iat: now,
        exp: now + TTL_SECONDS,
        name: name.to_s,
        email: email.to_s,
        roles: Array(roles)
      }
      JWT.encode(payload, AppConfig.local_auth_hmac_secret, "HS256")
    end
  end
end
