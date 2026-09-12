# frontend-railsはbffを経由せず、Rails自身がOIDCクライアントとしてKeycloakと
# Authorization Codeフローを直接行う(既存のfrontend+bffのBFFパターンとの比較実装)
#
# CONTRACT.md: frontend-rails.oidc-gem(2値)で、omniauth-openid-connect(お任せ型)/
# openid_connect(手組み型)のどちらでOIDCハンドシェイクを行うかを切り替える
# 実際の出し分けは app/middleware/oidc_gem_dispatcher.rb が担う(このファイルでは
# OmniAuth::Builderの構築をそちらへ委譲するだけ)
#
# client_id/secret/redirect_uriは bff/keycloak/realm-export.json の
# "frontend-rails" クライアント定義と一致させること(どちらのgemを使っても
# コールバックパス/auth/openid_connect/callbackは変わらないため、Keycloak側の
# redirect_uri登録も1つのままで良い)

# 【実機検証で判明】config/initializers/*はZeitwerkのオートローダーがまだ完全には
# セットアップされていない段階で読まれるため、この初期化処理が直接参照するクラスの
# 自動読み込みが効かずNameError(uninitialized constant)になる
# 明示的に require する
#
# OidcGemDispatcher 内部からの
# OidcGemFlag/OidcDiscovery参照はメソッド呼び出し時 = リクエスト処理時になるため、オートローダーが使えるようになっており問題ない
require_relative "../../app/services/oidc_gem_flag"
require_relative "../../app/services/oidc_discovery"
require_relative "../../app/middleware/oidc_gem_dispatcher"

Rails.application.config.middleware.use OidcGemDispatcher

OmniAuth.config.allowed_request_methods = [:post]
# 失敗時は既定通り "/auth/failure" へリダイレクトされる(OmniAuth::FailureEndpointの既定動作)

# テスト時はネットワークへポーリングしない(specでOidcGemFlag.override!を使う)
OidcGemFlag.start_polling! unless Rails.env.test?
