# frontend-rails.oidc-gem(2値: omniauth-openid-connect / openid_connect)を、
# backendの GET /internal/v1/feature-flags/export をポーリングして評価する
# bff(bff/internal/featureflag/evaluator.go)・gateway(gateway/nginx/sidecar)と同じ
# 「exportエンドポイントをHTTPでポーリングする」方式を踏襲し、MySQLへの直接接続は増やさない
#
# バックグラウンドスレッドで10秒間隔ポーリングし、直近の値をスレッドセーフにキャッシュする
# エクスポートAPIに到達できない場合は既定値(omniauth-openid-connect)にフォールバックする
require "net/http"
require "json"

class OidcGemFlag
  FLAG_KEY = "frontend-rails.oidc-gem"
  DEFAULT_VALUE = "omniauth-openid-connect"
  VALID_VALUES = %w[omniauth-openid-connect openid_connect].freeze

  class << self
    def current
      @mutex ||= Mutex.new
      @mutex.synchronize { @value || DEFAULT_VALUE }
    end

    # テスト・手動確認用に値を強制上書きする(pollerを止めずに済む)
    def override!(value)
      @mutex ||= Mutex.new
      @mutex.synchronize { @value = value }
    end

    def start_polling!
      return if @started
      @started = true
      poll_once
      @thread = Thread.new do
        loop do
          sleep interval_seconds
          poll_once
        end
      end
      @thread.abort_on_exception = false
    end

    def poll_once
      uri = URI.parse(export_url)
      req = Net::HTTP::Get.new(uri)
      req["X-Feature-Flag-Poll-Token"] = poll_token
      res = Net::HTTP.start(uri.host, uri.port, open_timeout: 3, read_timeout: 3) { |http| http.request(req) }
      raise "export API status=#{res.code}" unless res.is_a?(Net::HTTPSuccess)

      parsed = JSON.parse(res.body)
      variation = parsed.dig(FLAG_KEY, "defaultRule", "variation")
      disabled = parsed.dig(FLAG_KEY, "disable")
      value = (disabled || variation.blank? || !VALID_VALUES.include?(variation)) ? DEFAULT_VALUE : variation

      @mutex ||= Mutex.new
      @mutex.synchronize { @value = value }
    rescue StandardError => e
      Rails.logger.warn("frontend-rails.oidc-gemのポーリングに失敗しました(既定値を使用): #{e.message}") if defined?(Rails)
      @mutex ||= Mutex.new
      @mutex.synchronize { @value ||= DEFAULT_VALUE }
    end

    private

    def export_url
      ENV.fetch("FEATURE_FLAG_EXPORT_URL", "http://localhost:8090/internal/v1/feature-flags/export")
    end

    def poll_token
      ENV.fetch("FEATURE_FLAG_POLL_TOKEN", "local-dev-feature-flag-poll-token")
    end

    def interval_seconds
      Integer(ENV.fetch("FEATURE_FLAG_POLL_INTERVAL_SECONDS", "10"))
    end
  end
end
