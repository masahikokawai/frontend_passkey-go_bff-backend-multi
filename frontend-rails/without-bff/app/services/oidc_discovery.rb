# KeycloakのOIDC discoveryドキュメント(/.well-known/openid-configuration)を取得する共通ヘルパー
# omniauth-openid-connect(内部で自動的にdiscoveryする)とは独立して、
# 手組みのopenid_connect gem実装・ログアウト時のend_session_endpoint取得の両方から使う
module OidcDiscovery
  def self.issuer
    ENV.fetch("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training")
  end

  def self.provider_config
    ::OpenIDConnect::Discovery::Provider::Config.discover!(issuer)
  end
end
