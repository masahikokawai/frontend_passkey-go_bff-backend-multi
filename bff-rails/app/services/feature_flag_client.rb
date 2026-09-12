# backendの GET /internal/v1/feature-flags/export を定期ポーリングし、
# frontend-rails.oidc-gem の現在値をキャッシュする(CONTRACT.md参照、bff(Go)の
# featureflag.Evaluatorと同じ「exportエンドポイントをポーリングする」経路を踏襲し、
# MySQLへの直接接続は増やさない)
#
# rails serverはリクエスト都度スレッドで動くため、バックグラウンドポーリングスレッドではなく
# 「最後に取得してからpoll_interval秒以上経っていたら次回アクセス時に再取得する」という
# 遅延評価キャッシュにしている(実装をシンプルに保つための判断)
class FeatureFlagClient
  DEFAULT_VARIATION = "omniauth-openid-connect"
  FLAG_KEY = "frontend-rails.oidc-gem"

  class << self
    def oidc_gem
      refresh_if_stale
      @value || DEFAULT_VARIATION
    end

    # テスト・強制リフレッシュ用
    def reset!
      @value = nil
      @fetched_at = nil
    end

    private

    def refresh_if_stale
      return if @fetched_at && (Time.now - @fetched_at) < AppConfig.feature_flag_poll_interval_seconds

      @value = fetch_value || @value
      @fetched_at = Time.now
    rescue StandardError => e
      Rails.logger.warn("feature flag export取得に失敗しました: #{e.message}") if defined?(Rails)
      @fetched_at = Time.now # 失敗時も無限リトライを避けるため、次のポーリング間隔まで待つ
    end

    def fetch_value
      conn = Faraday.new(url: AppConfig.feature_flag_export_url) do |f|
        f.options.timeout = 3
        f.options.open_timeout = 3
      end
      resp = conn.get do |req|
        req.headers["X-Feature-Flag-Poll-Token"] = AppConfig.feature_flag_poll_token
      end
      return nil unless resp.success?

      body = JSON.parse(resp.body)
      variation = body.dig(FLAG_KEY, "defaultRule", "variation")
      variation.presence
    end
  end
end
