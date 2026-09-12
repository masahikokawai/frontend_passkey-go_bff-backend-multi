require "faraday"

module Auth
  # 【2回目のテスト監査で発見・修正】bff-railsには、Keycloakのaccess_tokenが期限切れに
  # なった場合の更新処理が一切存在しなかった。Keycloakのaccess_tokenは5分(realm-export.json
  # の accessTokenLifespan: 300)で切れる一方、bff-railsのセッションCookie自体は
  # SessionStore::TTL_SECONDS(30分)生きているため、ログインから5分を過ぎるとTask一覧取得が
  # 毎回502になる、という実際に発生しうるギャップだった(bff(Go)の`auth.Refresher`が
  # 同じ状況をtoken refresh+1回だけ再試行することで解決しているのに、bff-railsには
  # 対応する仕組みが無かった)。
  #
  # ここではgem(omniauth-openid-connect/openid_connect)非依存の素朴なFaraday呼び出しで
  # 実装する。refresh_tokenグラントはOIDCのgemが提供する高レベルAPIを介さなくても
  # 標準化されたただのPOSTであり、ログイン時に使ったgemが何であってもこの1つの実装で
  # 共通に扱える(ログインgemの選択(frontend-rails.oidc-gem)とリフレッシュ方式を
  # 独立させておくことで、gem切り替えのたびにリフレッシュ処理を複製せずに済む)
  module TokenRefresher
    class Error < StandardError; end

    module_function

    # refresh_token: 現在のセッションに保存されているKeycloak発行のrefresh_token
    # 戻り値: Auth::TokenResult相当のHash(access_token/refresh_token)
    # 失敗時(refresh_token自体が失効・無効化されている等)はErrorを送出する
    def refresh(refresh_token)
      resp = Faraday.post("#{AppConfig.keycloak_issuer}/protocol/openid-connect/token") do |req|
        req.headers["Content-Type"] = "application/x-www-form-urlencoded"
        req.body = URI.encode_www_form(
          grant_type: "refresh_token",
          refresh_token: refresh_token,
          client_id: AppConfig.keycloak_client_id,
          client_secret: AppConfig.keycloak_client_secret
        )
      end
      raise Error, "token refresh failed: status=#{resp.status} body=#{resp.body}" unless resp.success?

      body = JSON.parse(resp.body)
      { access_token: body.fetch("access_token"), refresh_token: body.fetch("refresh_token", refresh_token) }
    rescue Faraday::Error => e
      # 【WHY】Keycloak自体に接続できない場合も、呼び出し側からは「refreshできなかった」
      # という1種類の失敗として扱えるよう、Faraday::ErrorもTokenRefresher::Errorに包む
      raise Error, "token refresh request failed: #{e.message}"
    end
  end
end
