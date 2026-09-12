require "openid_connect"
require "omniauth-openid-connect"

module Auth
  # frontend-rails.oidc-gem = "omniauth-openid-connect" のときに使う実装
  #
  # 【設計上の注記】omniauth-openid-connectは本来Rackミドルウェアとして常駐させ、
  # ブラウザのセッション(Rackセッション)にstate/nonceを保存する設計だが、bff-railsは
  # API-onlyアプリでリクエスト間の従来型セッションを持たない(BFFパターンとしてRedisだけに
  # 状態を持つ設計のため)。そのため`OmniAuth::Strategies::OpenIDConnect`のRack
  # ミドルウェアとしての`call`/`request_phase`/`callback_phase`は使わず、同クラスが
  # 内部で使っている`client`(`OpenIDConnect::Client`の構築)・token交換・ID Token検証の
  # ロジックだけを、gemの設定規約(`client_options`/`issuer`/`discovery`)に沿って呼び出す。
  # state/nonceの保管は`Auth::PendingLoginStore`(Redis)が担う
  class OmniauthGemLogin
    def authorize_url(state:, nonce:, code_challenge:)
      client.authorization_uri(
        response_type: "code",
        scope: [:openid, :profile, :email],
        state: state,
        nonce: nonce,
        code_challenge: code_challenge,
        code_challenge_method: "S256"
      )
    end

    # 認可コードをtokenへ交換し、ID Tokenを検証したうえでクレームを返す
    def exchange_code(code:, nonce:, code_verifier:)
      client.authorization_code = code
      access_token = client.access_token!(client_auth_method: :basic, code_verifier: code_verifier)

      id_token = ::OpenIDConnect::ResponseObject::IdToken.decode(access_token.id_token, jwks)
      id_token.verify!(issuer: AppConfig.keycloak_issuer, client_id: AppConfig.keycloak_client_id, nonce: nonce)

      Auth::TokenResult.new(
        access_token: access_token.access_token,
        refresh_token: access_token.refresh_token,
        sub: id_token.sub,
        name: id_token.raw_attributes["name"] || id_token.raw_attributes["preferred_username"],
        email: id_token.raw_attributes["email"]
      )
    end

    private

    # OmniAuth::Strategies::OpenIDConnectのインスタンスを、Rackミドルウェアとしてではなく
    # 「clientビルダー」としてのみ使う(#client)。appにはダミーのRackアプリを渡す
    def strategy
      @strategy ||= OmniAuth::Strategies::OpenIDConnect.new(
        ->(_env) { [404, {}, []] },
        client_options: {
          identifier: AppConfig.keycloak_client_id,
          secret: AppConfig.keycloak_client_secret,
          redirect_uri: AppConfig.redirect_uri,
          scheme: issuer_uri.scheme,
          host: issuer_uri.host,
          port: issuer_uri.port,
          authorization_endpoint: "#{issuer_uri.path}/protocol/openid-connect/auth",
          token_endpoint: "#{issuer_uri.path}/protocol/openid-connect/token",
          userinfo_endpoint: "#{issuer_uri.path}/protocol/openid-connect/userinfo",
          jwks_uri: "#{issuer_uri.path}/protocol/openid-connect/certs"
        },
        issuer: AppConfig.keycloak_issuer,
        scope: [:openid, :profile, :email]
      )
    end

    def client
      strategy.client
    end

    def issuer_uri
      @issuer_uri ||= URI.parse(AppConfig.keycloak_issuer)
    end

    def jwks
      @jwks ||= begin
        raw = Faraday.get("#{AppConfig.keycloak_issuer}/protocol/openid-connect/certs").body
        JSON::JWK::Set.new(JSON.parse(raw))
      end
    end
  end
end
