# frontend-rails.oidc-gem=openid_connect(手組み型)の経路をテストするための、
# 偽Keycloak(discoveryドキュメント・JWKS・token endpoint)をWebMockでスタブするヘルパー
# 実際にRSA鍵ペアで署名したID Tokenを使い、ManualOidcClientの署名検証(JSON::JWK経由)まで本物同様に通す
module OidcTestHelpers
  ISSUER = "http://localhost:8082/realms/training".freeze

  def stub_fake_keycloak!(sub:, name:, email:, nonce:)
    rsa_key = OpenSSL::PKey::RSA.generate(2048)
    jwk = JSON::JWK.new(rsa_key, kid: "test-key-1")

    discovery_doc = {
      issuer: ISSUER,
      authorization_endpoint: "#{ISSUER}/protocol/openid-connect/auth",
      token_endpoint: "#{ISSUER}/protocol/openid-connect/token",
      jwks_uri: "#{ISSUER}/protocol/openid-connect/certs",
      end_session_endpoint: "#{ISSUER}/protocol/openid-connect/logout",
      response_types_supported: ["code"],
      subject_types_supported: ["public"],
      id_token_signing_alg_values_supported: ["RS256"],
    }

    stub_request(:get, "#{ISSUER}/.well-known/openid-configuration")
      .to_return(status: 200, body: discovery_doc.to_json, headers: { "Content-Type" => "application/json" })

    stub_request(:get, discovery_doc[:jwks_uri])
      .to_return(status: 200, body: { keys: [jwk.as_json] }.to_json, headers: { "Content-Type" => "application/json" })

    now = Time.now.to_i
    claims = {
      iss: ISSUER,
      sub: sub,
      aud: "frontend-rails",
      exp: now + 300,
      iat: now,
      nonce: nonce,
      name: name,
      email: email,
    }
    id_token_jwt = JSON::JWT.new(claims)
    id_token_jwt.kid = jwk[:kid]
    signed_id_token = id_token_jwt.sign(rsa_key, :RS256).to_s

    stub_request(:post, discovery_doc[:token_endpoint])
      .to_return(
        status: 200,
        body: { access_token: "dummy-access-token", token_type: "Bearer", id_token: signed_id_token }.to_json,
        headers: { "Content-Type" => "application/json" },
      )

    { discovery_doc: discovery_doc, signed_id_token: signed_id_token }
  end

  # CONTRACT.mdセクション22.9: ログイン成功時にbackendへJITプロビジョニングを行うようになったため、
  # 既存のログインテスト全てでこのスタブが必要になった(呼ばないとWebMockが未登録リクエストとして拒否する)
  def stub_backend_provision!(access_token: "dummy-access-token", user_id: 1)
    stub_request(:post, "#{AppConfig.backend_rest_base_url}/internal/v1/users/provision")
      .with(headers: { "Authorization" => "Bearer #{access_token}" })
      .to_return(status: 200, body: { user_id: user_id }.to_json, headers: { "Content-Type" => "application/json" })
  end
end

RSpec.configure do |config|
  config.include OidcTestHelpers, type: :request
end
