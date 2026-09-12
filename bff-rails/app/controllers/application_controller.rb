class ApplicationController < ActionController::API
  include ActionController::Cookies
  include SecurityHeaders

  # 【テスト監査で発見・修正】構文が壊れたJSONボディ(例: `{not valid json`)を
  # 送ると、Railsのparams解析(ActionDispatch::Http::Parameters)がJSON::ParserErrorを
  # ActionDispatch::Http::Parameters::ParseErrorとして送出し、rescue_fromが無いと
  # そのまま500になっていた(bff(Go)側はGinのShouldBindJSONのエラーを各ハンドラが
  # 明示的に400へマッピングしており、この非対称に気づいて追加した)
  rescue_from ActionDispatch::Http::Parameters::ParseError, with: :render_invalid_request_body

  private

  def render_invalid_request_body
    render json: { error: "invalid_request" }, status: :bad_request
  end

  def current_session
    @current_session ||= SessionStore.find(session_id_from_cookie)
  end

  def session_id_from_cookie
    cookies[:bff_rails_session]
  end

  def require_session!
    return if current_session

    render json: { error: "unauthorized" }, status: :unauthorized
  end

  # AuthController#callback(Keycloak経由)・PasskeysController#login_finish(パスキー経由)の
  # 両方から呼ばれる、セッション発行の共通処理
  def issue_session_cookie!(session)
    session_id = SessionStore.create(session)
    cookies[:bff_rails_session] = {
      value: session_id,
      httponly: true,
      same_site: :lax,
      secure: Rails.env.production?
    }
    session_id
  end
end
