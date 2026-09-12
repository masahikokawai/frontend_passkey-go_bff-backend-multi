# frontend-rails.oidc-gem = "openid_connect" のときに使う、手組みのOIDCクライアント
#
# omniauth-openid-connect(お任せ型、Rackミドルウェアがstate/nonce/リダイレクト/コールバックを
# 自動で処理する)との対比として、openid_connect gem単体(低レベルライブラリ)を直接使い、
# 認可リクエストの組み立て・state/nonceの発行と検証・code→tokenの交換・ID Tokenの署名検証(JWKS)を
# 全てこのクラスとSessionsControllerで手組みする
class ManualOidcClient
  def self.client
    config = OidcDiscovery.provider_config
    ::OpenIDConnect::Client.new(
      identifier: ENV.fetch("OIDC_CLIENT_ID", "frontend-rails"),
      secret: ENV.fetch("OIDC_CLIENT_SECRET", "frontend-rails-local-dev-secret"),
      redirect_uri: ENV.fetch("OIDC_REDIRECT_URI", "http://localhost:5174/auth/openid_connect/callback"),
      scheme: config.authorization_endpoint.start_with?("https") ? "https" : "http",
      host: URI.parse(config.authorization_endpoint).host,
      port: URI.parse(config.authorization_endpoint).port,
      authorization_endpoint: URI.parse(config.authorization_endpoint).request_uri,
      token_endpoint: URI.parse(config.token_endpoint).request_uri,
    )
  end

  # 認可エンドポイントへのリダイレクトURLを組み立てる
  # state/nonce は呼び出し側(コントローラ)でセッションに保存し、コールバック時にverify!へ渡してCSRF/リプレイを防ぐ
  #
  # 【実機検証で判明】bff/keycloak/realm-export.jsonの"frontend-rails"クライアントは
  # pkce.code.challenge.method=S256が設定されており、PKCE無しの認可リクエストは
  # Keycloakから invalid_request(Missing parameter: code_challenge_method) で即座に拒否される
  # omniauth-openid-connect 側は `pkce: true` オプションが自動で code_verifier/code_challenge を生成してくれるが、
  # 手組み実装ではここも自前で行う必要がある
  def self.authorization_uri(state:, nonce:, code_verifier:)
    code_challenge = Base64.urlsafe_encode64(Digest::SHA256.digest(code_verifier), padding: false)
    client.authorization_uri(
      response_type: :code,
      scope: [:openid, :profile, :email],
      state: state,
      nonce: nonce,
      code_challenge: code_challenge,
      code_challenge_method: "S256",
    )
  end

  # 認可コードをKeycloakのtoken endpointへ渡し、ID Tokenを検証済みの状態で取り出す
  # 戻り値は [検証済みIdTokenオブジェクト, 生のID Token JWT文字列, access_token文字列]
  # 生のJWT文字列はRP-Initiated Logoutのid_token_hintにそのまま使う
  #
  # 【CONTRACT.mdセクション22.9で追加】access_tokenも返すよう戻り値を拡張した。
  # パスキー機能のJITプロビジョニング・webauthn登録APIをbackendへ直接呼ぶために必要
  # (21.2でKeycloakの"frontend-rails"クライアントにoidc-audience-mapperを追加済みのため、
  # このaccess_tokenはaud=backendを持つ)
  def self.exchange_code_for_id_token!(code:, expected_nonce:, code_verifier:)
    c = client
    c.authorization_code = code
    access_token = c.access_token!(client_auth_method: :basic, code_verifier: code_verifier)
    raw_id_token = access_token.id_token

    id_token = ::OpenIDConnect::ResponseObject::IdToken.decode(raw_id_token, OidcDiscovery.provider_config)
    id_token.verify!(issuer: OidcDiscovery.issuer, client_id: ENV.fetch("OIDC_CLIENT_ID", "frontend-rails"), nonce: expected_nonce)
    [id_token, raw_id_token, access_token.access_token]
  end

  def self.generate_code_verifier
    SecureRandom.urlsafe_base64(48)
  end
end
