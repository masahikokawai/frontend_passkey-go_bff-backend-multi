# frontend-rails.oidc-gem の現在値に応じて、OmniAuthのRackミドルウェア(お任せ型)へ
# リクエストを渡すか、Railsルーティングへそのまま素通しして手組みのopenid_connect gem実装
# (SessionsController#start_login/#omniauth_callback)に処理させるかを切り替える
#
# OmniAuth::Builderは`/auth/openid_connect`(request phase)・`/auth/openid_connect/callback`
# (callback phase)というパスに一致するリクエストを、Railsルーティングより手前で横取りする
# 設計のため、「flagがopenid_connectのときはOmniAuth::Builderに一切触らせない」ようにするには
# このように外側でRackアプリを丸ごと出し分ける必要がある(ミドルウェアを1つだけ動的に
# on/offする簡単な方法が無いため)
class OidcGemDispatcher
  OIDC_PATHS = ["/auth/openid_connect", "/auth/openid_connect/callback"].freeze

  def initialize(app)
    @app = app
    @omniauth_app = build_omniauth_app(app)
  end

  def call(env)
    path = env["PATH_INFO"]
    if OIDC_PATHS.include?(path) && OidcGemFlag.current == "omniauth-openid-connect"
      @omniauth_app.call(env)
    else
      @app.call(env)
    end
  end

  private

  def build_omniauth_app(app)
    issuer_url = OidcDiscovery.issuer

    # 【実機検証で判明】openid_connect gemが内部で使うswd gemは、discoveryドキュメント
    # ("/.well-known/openid-configuration")の取得先URLのschemeを無視し、常に
    # URI::HTTPS決め打ちで組み立てる(swd/lib/swd.rbのurl_builder既定値)
    # ローカル開発の Keycloak は平文HTTPのため、そのままだとTLSハンドシェイクで失敗する
    # (OpenSSL::SSL::SSLError: record layer failure)
    # issuer が http:// の場合のみ discovery 先の URL組み立ても HTTP へ切り替える
    SWD.url_builder = URI::HTTP if issuer_url.start_with?("http://")

    OmniAuth::Builder.new(app) do
      provider :openid_connect,
        name: :openid_connect,
        issuer: issuer_url,
        discovery: true,
        scope: [:openid, :profile, :email],
        pkce: true,
        client_options: {
          identifier: ENV.fetch("OIDC_CLIENT_ID", "frontend-rails"),
          secret: ENV.fetch("OIDC_CLIENT_SECRET", "frontend-rails-local-dev-secret"),
          redirect_uri: ENV.fetch("OIDC_REDIRECT_URI", "http://localhost:5174/auth/openid_connect/callback"),
        }
    end
  end
end
