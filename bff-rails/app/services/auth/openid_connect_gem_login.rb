require "openid_connect"

module Auth
  # frontend-rails.oidc-gem = "openid_connect" のときに使う実装
  #
  # omniauth-openid-connectのようなStrategy抽象を経由せず、openid_connect gemの
  # OpenIDConnect::Client / OpenIDConnect::Discovery::Provider::Config /
  # OpenIDConnect::ResponseObject::IdToken を直接組み立てて使う「手組み型」。
  # コード量はOmniauthGemLoginとほぼ同じになるが、gemの設定規約(client_options等)に
  # 頼らず全てのパラメータを自分で渡す点が異なる
  class OpenidConnectGemLogin
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

    def client
      @client ||= ::OpenIDConnect::Client.new(
        identifier: AppConfig.keycloak_client_id,
        secret: AppConfig.keycloak_client_secret,
        redirect_uri: AppConfig.redirect_uri,
        scheme: issuer_uri.scheme,
        host: issuer_uri.host,
        port: issuer_uri.port,
        authorization_endpoint: "#{issuer_uri.path}/protocol/openid-connect/auth",
        token_endpoint: "#{issuer_uri.path}/protocol/openid-connect/token",
        userinfo_endpoint: "#{issuer_uri.path}/protocol/openid-connect/userinfo"
      )
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
