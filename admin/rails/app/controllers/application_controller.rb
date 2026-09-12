class ApplicationController < ActionController::Base
  # Only allow modern browsers supporting webp images, web push, badges, import maps, CSS nesting, and CSS :has.
  allow_browser versions: :modern

  # CONTRACT.mdセクション13: OIDCは導入せず、内部管理ツールとしてHTTP Basic Authのみで
  # 保護する(認証方式の比較はbackend/bffで既に十分行っているため、この2アプリでは
  # CRUD実装そのものの比較に集中する、という設計判断)
  http_basic_authenticate_with(
    name: ENV.fetch("ADMIN_BASIC_AUTH_USER", "admin"),
    password: ENV.fetch("ADMIN_BASIC_AUTH_PASSWORD", "password"),
    unless: -> { Rails.env.test? && !ENV["FORCE_BASIC_AUTH_IN_TEST"] }
  )

  # コントローラのspecでは basic_auth のたびにヘッダを組み立てるのが煩雑になるため、
  # test環境では既定で無効化し、専用のspecでだけ FORCE_BASIC_AUTH_IN_TEST=1 を付けて
  # 有効化を確認する
  helper_method :current_admin_name

  def current_admin_name
    request.env["REMOTE_USER"] || ENV.fetch("ADMIN_BASIC_AUTH_USER", "admin")
  end

  # 実機検証で発覚した不具合: 存在しないidへアクセスすると、以前はRailsの生の例外ページ
  # (Action Controller: Exception caught)がそのまま表示され、一覧へ戻る手段が無かった
  #
  # admin/go側は同じケースを「一覧へ戻る」リンク付きのエラー表示にしているため、
  # 動作を揃える(root_pathへリダイレクトし、flashでエラーを表示する
  # リダイレクト先は常にナビゲーションバー付きの画面なので、戻る手段が無い状態には陥らない)
  rescue_from ActiveRecord::RecordNotFound, with: :render_record_not_found

  private

  def render_record_not_found(exception)
    redirect_to root_path, flash: { danger: "指定されたデータが見つかりません: #{exception.message}" }
  end
end
