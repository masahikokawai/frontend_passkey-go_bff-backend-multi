class TasksController < ApplicationController
  before_action :require_session!

  # GET /api/tasks
  #
  # 【2回目のテスト監査で修正】以前はbackendからの401/403も「backend_error」として
  # そのまま502を返すだけで、Keycloakのaccess_tokenが5分(realm-export.jsonの
  # accessTokenLifespan)で切れた後、bff-railsのセッションCookie自体はまだ30分
  # (SessionStore::TTL_SECONDS)生きている、という期間中ずっとTask一覧取得が
  # 壊れたままになる実際のギャップだった。bff(Go)の`auth.Refresher`と同じ
  # 「1回だけtoken refreshして再試行、失敗したらセッションを破棄して401」という
  # 挙動をここで再現する
  def index
    render json: fetch_tasks(current_session)
  rescue BackendClient::UnauthorizedError
    refreshed = try_refresh_and_retry
    if refreshed
      render json: refreshed
    else
      destroy_session_and_respond_unauthorized!
    end
  rescue BackendClient::Error, Faraday::Error => e
    # 【WHY】Faraday::Error(接続不能・タイムアウト等)を捕捉し損ねると、Railsの素の
    # 例外ページ(スタックトレース含む)がそのまま返ってしまう。認証エラーとは別物として
    # 502で返し、401系へ誤分類しないことも重要(誤って再ログインを促すべきではない)
    render json: { error: "backend_error", detail: e.message }, status: :bad_gateway
  end

  private

  def fetch_tasks(session)
    BackendClient.new(session.access_token).list_tasks
  end

  # refreshできた場合はTask一覧のJSONを返す。refreshできない/対象外の場合はnil
  def try_refresh_and_retry
    session = current_session
    return nil unless session.auth_mode == "keycloak" && session.refresh_token.present?

    begin
      tokens = Auth::TokenRefresher.refresh(session.refresh_token)
    rescue Auth::TokenRefresher::Error
      return nil
    end

    session.access_token = tokens[:access_token]
    session.refresh_token = tokens[:refresh_token]
    SessionStore.update(session_id_from_cookie, session)
    @current_session = session

    begin
      fetch_tasks(session)
    rescue BackendClient::Error, Faraday::Error
      # 【WHY】refresh自体は成功したのに再試行がまた失敗する場合、backend側の別の問題
      # (一時的な障害等)の可能性が高く、無限にrefreshを繰り返す意味は無いため、
      # ここでは1回だけ再試行してそれ以上は追わない(bffのRefresher.Doと同じ方針)
      nil
    end
  end

  def destroy_session_and_respond_unauthorized!
    SessionStore.destroy(session_id_from_cookie)
    cookies.delete(:bff_rails_session)
    render json: { error: "unauthorized" }, status: :unauthorized
  end
end
