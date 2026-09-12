-- frontend-rails(without-bff)・bff-rails(with-bff)が、それぞれ自前でKeycloakとの
-- OIDCハンドシェイクを行う際に使うgemを切り替えるRelease Toggle
-- 「omniauth-openid-connect(Rackミドルウェアがstate/nonce/リダイレクトを面倒見る、お任せ型)」と
-- 「openid_connect(低レベルライブラリを自分で組み立てる型)」という2つの排他的な実装から
-- 1つを選ぶ多値flag。既存の多値flag(variations列)の枠組みにそのまま乗せる。
INSERT INTO feature_flags (flag_key, description, default_variation, enabled, variations, created_at, updated_at)
VALUES (
  'frontend-rails.oidc-gem',
  'frontend-rails/without-bffおよびbff-rails(frontend-rails/with-bff用)が、KeycloakとのOIDCハンドシェイクにどちらのgemを使うかを切り替えるRelease Toggle。omniauth-openid-connect(Rackミドルウェアが自動でstate/nonce/リダイレクトを扱うお任せ型)/openid_connect(低レベルライブラリを自分で組み立てる型)の2択。',
  'omniauth-openid-connect',
  1,
  JSON_OBJECT('omniauth-openid-connect', 'omniauth-openid-connect', 'openid_connect', 'openid_connect'),
  NOW(),
  NOW()
);
