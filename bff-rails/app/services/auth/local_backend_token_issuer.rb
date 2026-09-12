require "jwt"

module Auth
  # パスキーのみでログインしたセッションには、backend呼び出しに使えるOIDCの
  # access_tokenが存在しない(パスキーはKeycloakを経由しないため)。
  #
  # これを解決するため、bff(Go)のinternal/auth/local_jwt.go(ローカルHMAC認証、
  # CONTRACT.mdセクション16.4)と全く同じクレーム形状・署名方式(HS256、
  # 共有シークレット)で自前のJWTを発行し、それをそのままbackendへ転送する
  # access_token代わりに使う。backend側のauthjwt.HMACVerifierは「iss=bff-gin-local-hmacの
  # 署名が正しいか」だけを見ており、発行元がbff(Go)かbff-railsかを区別しないため、
  # 同じ共有シークレットを設定するだけでbackend側のコード変更は一切不要になる
  # (bffが自分のローカル認証ユーザー向けに使っている信頼境界を、そのまま間借りする形)。
  #
  # 有効期限はbff(Go)のlocalTokenTTLと同じ24時間にしている。パスキーログインには
  # refresh_tokenという概念が無いが、bff-railsは自分の秘密鍵(共有シークレット)を
  # 常に持っているため、期限が近づいたら単に新しいJWTを署名し直せばよく、
  # OIDCのrefresh_token往復のような複雑さは発生しない(このメソッドを都度呼べばよい)。
  module LocalBackendTokenIssuer
    # backend側のauthjwt.Dispatcherがこのissuer文字列でHMACVerifierへ振り分ける
    # (bff/internal/auth/local_jwt.goのLocalHMACIssuerと完全に一致させる必要がある)
    ISSUER = "bff-gin-local-hmac".freeze
    # backendのExpectedAudienceと一致させる
    AUDIENCE = "backend".freeze
    # bff(Go)のlocalTokenTTL(24時間)と揃える
    TTL_SECONDS = 24 * 60 * 60

    module_function

    # user_id: backend内部のユーザーID(文字列化してsubクレームに入れる)
    # name/email/roles: セッション表示・role判定に使う値(rolesは配列)
    def issue(user_id:, name:, email:, roles:)
      now = Time.now.to_i
      payload = {
        # RegisteredClaims相当。golang-jwt/v5のClaimStrings(aud)は配列を
        # 受け付けるため["backend"]の形で送る(単一文字列でもGo側はパースできるが、
        # 複数audienceを持たせる将来の拡張に備えて配列で統一する)
        iss: ISSUER,
        sub: user_id.to_s,
        aud: [AUDIENCE],
        iat: now,
        exp: now + TTL_SECONDS,
        # bffのLocalClaims(RegisteredClaims埋め込み + Name/Email/Roles)と同じ追加クレーム
        name: name.to_s,
        email: email.to_s,
        roles: Array(roles)
      }
      JWT.encode(payload, AppConfig.local_auth_hmac_secret, "HS256")
    end
  end
end
