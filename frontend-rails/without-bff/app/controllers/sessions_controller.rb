# frontend-railsの認証はbffを経由しない
# RailsのCookieセッション(session[:user])だけで完結する(CONTRACT.md参照: 既存frontend+bffのBFFパターンとの比較実装)
#
# frontend-rails.oidc-gem(2値)で、OIDCハンドシェイクの実装を丸ごと切り替える
# どちらの実装でもコールバックパスは/auth/openid_connect/callbackで共通(Keycloak側のredirect_uri登録を1つに保つため)
# OidcGemDispatcher(app/middleware)が、現在のflag値に応じてOmniAuthミドルウェアへ渡すか、
# この #start_login/#omniauth_callback へ素通しするかを出し分けている
class SessionsController < ApplicationController
  skip_before_action :verify_authenticity_token, only: [:omniauth_callback]

  before_action :require_login, only: %i[welcome account]
  before_action :redirect_if_logged_in, only: [:login]

  def login
  end

  def welcome
  end

  # GET /account(CONTRACT.mdセクション22.9: パスキー登録画面。要ログイン)
  def account
  end

  # frontend-rails.oidc-gem=openid_connect(手組み型)のときだけ到達する
  # omniauth-openid-connect(お任せ型)選択時はOmniAuth::Builderがこのパスを横取りして
  # 自分でKeycloakへリダイレクトするため、このアクションには来ない
  def start_login
    state = SecureRandom.hex(16)
    nonce = SecureRandom.hex(16)
    code_verifier = ManualOidcClient.generate_code_verifier
    session[:oidc_state] = state
    session[:oidc_nonce] = nonce
    session[:oidc_code_verifier] = code_verifier
    redirect_to ManualOidcClient.authorization_uri(state: state, nonce: nonce, code_verifier: code_verifier), allow_other_host: true
  end

  # OmniAuthのcallback phase(request.env["omniauth.auth"]が設定されている)/
  # 手組みの openid_connect gem による callback(params[:code]/params[:state]が渡ってくる)の両方をこの1つのアクションで受ける
  # どちらの経路で来たかは request.env["omniauth.auth"] の有無で判別する
  def omniauth_callback
    if (auth = request.env["omniauth.auth"])
      # 【3回目のテスト監査(セキュリティ)で発見・修正】以前はここでreset_sessionを
      # 呼んでいなかったため、Session Fixation(セッション固定化)の余地があった
      #
      # ログイン前に発行されたsession_idを、ログイン成功後もそのまま使い続けると、
      # 攻撃者が事前に(自分がアクセスして得た)session_idを被害者に踏ませ、
      # 被害者がそのセッションのままログインしてしまえば、攻撃者は自分が最初から
      # 知っているsession_idで被害者のログイン済みセッションへアクセスできてしまう
      #
      # reset_sessionでログイン成立の瞬間にRailsのセッションCookie自体を
      # 新しいIDへ差し替えることで、ログイン前のsession_idが以後無効になり、
      # 上記の攻撃が成立しなくなる(OWASPのSession Fixation対策の定石)
      reset_session
      access_token = auth.credentials&.token
      provisioned = provision_backend_user!(access_token)
      session[:user] = {
        "sub" => auth.uid,
        "user_id" => provisioned["user_id"],
        "name" => auth.info&.name,
        "email" => auth.info&.email,
      }
      session[:id_token] = auth.credentials&.id_token
      session[:access_token] = access_token
      session[:auth_mode] = "keycloak"
    else
      handle_manual_callback
    end
    redirect_to welcome_path, notice: "ログインしました"
  rescue StandardError => e
    Rails.logger.warn("OIDCコールバック処理に失敗しました: #{e.message}")
    redirect_to login_path, alert: "ログインに失敗しました(#{e.message})"
  end

  def omniauth_failure
    redirect_to login_path, alert: "ログインに失敗しました(#{params[:message]})"
  end

  def logout
    id_token = session[:id_token]
    reset_session

    # id_tokenが無ければRP-Initiated Logoutを試みる意味が無い(Keycloakはid_token_hint or
    # クライアント認証のいずれかを要求する)ため、discoveryへの問い合わせ自体を省略する
    end_session_endpoint = id_token.present? ? fetch_end_session_endpoint : nil
    if end_session_endpoint.present?
      post_logout_redirect_uri = "#{request.base_url}/login"
      logout_url = "#{end_session_endpoint}?post_logout_redirect_uri=#{ERB::Util.url_encode(post_logout_redirect_uri)}"
      logout_url += "&id_token_hint=#{ERB::Util.url_encode(id_token)}" if id_token.present?
      redirect_to logout_url, allow_other_host: true
    else
      redirect_to login_path, notice: "ログアウトしました"
    end
  end

  private

  # frontend-rails.oidc-gem=openid_connect(手組み型)のコールバック処理
  # state検証→code/token交換→ID Token署名検証(JWKS)→sessionへ格納、まで自分で行う
  def handle_manual_callback
    raise "stateが一致しません" if params[:state].blank? || params[:state] != session[:oidc_state]

    id_token, raw_id_token, access_token = ManualOidcClient.exchange_code_for_id_token!(
      code: params[:code],
      expected_nonce: session[:oidc_nonce],
      code_verifier: session[:oidc_code_verifier],
    )
    provisioned = provision_backend_user!(access_token)
    # 【3回目のテスト監査(セキュリティ)で発見・修正、omniauth_callbackの同名コメント参照】
    # state/nonce/code_verifierの検証・token交換が全て終わった直後、
    # session[:user]を書き込む前にreset_sessionを呼ぶ(Session Fixation対策)
    #
    # oidc_state等は既に上のexchange_code_for_id_token!呼び出しで使い終わっているため、
    # ここでリセットしても検証ロジックへの影響は無い(reset_session後のensure節での
    # session.deleteは、既に空になったキーへの削除なので無害)
    reset_session
    session[:user] = {
      "sub" => id_token.sub,
      "user_id" => provisioned["user_id"],
      "name" => id_token.raw_attributes["name"] || id_token.raw_attributes["preferred_username"],
      "email" => id_token.raw_attributes["email"],
    }
    session[:id_token] = raw_id_token
    session[:access_token] = access_token
    session[:auth_mode] = "keycloak"
  ensure
    session.delete(:oidc_state)
    session.delete(:oidc_nonce)
    session.delete(:oidc_code_verifier)
  end

  # CONTRACT.mdセクション22.9: パスキーのcredential保存先(backendのwebauthn_credentials
  # テーブル)に紐づけるため、Keycloakログイン成功のたびにbackend側のuser_idをJIT
  # プロビジョニングで確定させる(bff-rails/AuthController#callbackと同じ流れ)
  def provision_backend_user!(access_token)
    BackendClient.new(access_token).provision!
  rescue BackendClient::Error, Faraday::Error => e
    raise "backendへのユーザー登録に失敗しました(#{e.message})"
  end

  def require_login
    redirect_to login_path, alert: "ログインしてください" unless session[:user].present?
  end

  def redirect_if_logged_in
    redirect_to welcome_path if session[:user].present?
  end

  # KeycloakのRP-Initiated LogoutのためにOIDC discoveryドキュメントから
  # end_session_endpointを取得する(既存bff実装(bff/internal/auth)と同じ考え方)
  # 取得に失敗してもログアウト自体は継続できるようにレスキューする
  def fetch_end_session_endpoint
    OidcDiscovery.provider_config.end_session_endpoint
  rescue StandardError => e
    Rails.logger.warn("end_session_endpointの取得に失敗しました: #{e.message}")
    nil
  end
end
