require "openssl"

# テスト用にRS256署名済みのID Tokenと、それを検証するためのJWKSを組み立てるヘルパー
# (実際のKeycloakの代わりに、テストでは自前の鍵ペアで署名する)
module IdTokenHelper
  def build_signed_id_token(sub:, nonce:, name: "Taro Yamada", email: "taro@example.com")
    key = OpenSSL::PKey::RSA.generate(2048)
    jwk = JSON::JWK.new(key.public_key)
    claims = {
      iss: AppConfig.keycloak_issuer,
      sub: sub,
      aud: AppConfig.keycloak_client_id,
      exp: 5.minutes.from_now.to_i,
      iat: Time.now.to_i,
      nonce: nonce,
      name: name,
      email: email
    }
    jwt = JSON::JWT.new(claims)
    jwt.kid = jwk[:kid]
    signed = jwt.sign(key, :RS256)
    [signed.to_s, JSON::JWK::Set.new([jwk]).as_json]
  end
end

RSpec.configure do |config|
  config.include IdTokenHelper
end
