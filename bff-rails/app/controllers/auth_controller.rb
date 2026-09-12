class AuthController < ApplicationController
  # GET /api/auth/login
  def login
    gem_name = FeatureFlagClient.oidc_gem
    pending = Auth::PendingLoginStore.create(gem: gem_name)
    url = Auth::Strategy.for(gem_name).authorize_url(
      state: pending.state, nonce: pending.nonce, code_challenge: pending.code_challenge
    )
    redirect_to url, allow_other_host: true
  end

  # GET /auth/openid_connect/callback
  def callback
    if params[:error].present?
      render json: { error: "oidc_error", detail: params[:error_description] || params[:error] }, status: :bad_gateway
      return
    end

    pending = Auth::PendingLoginStore.consume(params[:state])
    if pending.nil?
      render json: { error: "invalid_state" }, status: :unauthorized
      return
    end

    result = Auth::Strategy.for(pending.gem).exchange_code(
      code: params[:code], nonce: pending.nonce, code_verifier: pending.code_verifier
    )

    backend = BackendClient.new(result.access_token)
    provisioned = backend.provision!

    session = SessionStore::Session.new(
      user_id: provisioned["user_id"],
      keycloak_sub: result.sub,
      name: result.name,
      email: result.email,
      access_token: result.access_token,
      refresh_token: result.refresh_token,
      auth_mode: "keycloak"
    )
    issue_session_cookie!(session)
    redirect_to AppConfig.frontend_origin, allow_other_host: true
  rescue BackendClient::Error, Faraday::Error => e
    render json: { error: "backend_error", detail: e.message }, status: :bad_gateway
  end

  # POST /api/auth/logout
  def logout
    # 【実装時に発見した実際のバグ】cookies.deleteを先に呼ぶと、現在のリクエスト中の
    # cookies読み取り結果も即座に空になり、後段のsession_id_from_cookieがnilを返して
    # SessionStore.destroyが何もしないno-opになっていた。必ずCookieを読んでから削除する
    session_id = session_id_from_cookie
    cookies.delete(:bff_rails_session)
    SessionStore.destroy(session_id)
    render json: {
      logout_url: "#{AppConfig.keycloak_issuer}/protocol/openid-connect/logout?" \
                   "post_logout_redirect_uri=#{ERB::Util.url_encode(AppConfig.post_logout_redirect_uri)}&" \
                   "client_id=#{AppConfig.keycloak_client_id}"
    }
  end
end
