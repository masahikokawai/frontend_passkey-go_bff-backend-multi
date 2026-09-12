# backend.external-tasks-pagination-v2 の評価(CONTRACT.mdセクション11・20.9)
#
# 5言語で共有する1つのflagのため、Go実装(featureflag.NewMySQLEvaluator、
# FeatureFlagPollInterval既定10秒)と同じポーリング間隔でMySQLを直接評価する
# (bff/gatewayのようなHTTPポーリングではなく、backend自身がDBを正本として直接見る設計を踏襲)
#
# admin/go・admin/railsからの変更が、次のポーリングまでの最大POLL_INTERVAL秒だけ遅れて反映される(Go実装と同じ「即時ではない」遅延特性)
class ExternalPaginationFlag
  FLAG_KEY = "backend.external-tasks-pagination-v2".freeze
  POLL_INTERVAL = (ENV["FEATURE_FLAG_POLL_INTERVAL_SECONDS"] || 10).to_i.seconds

  def self.instance
    @instance ||= new
  end

  def self.v2_enabled?
    instance.v2_enabled?
  end

  # テスト専用: キャッシュを破棄しシングルトンを作り直す
  def self.reset!
    @instance = nil
  end

  def initialize
    @mutex = Mutex.new
    @cached_value = nil
    @cached_at = nil
  end

  def v2_enabled?
    @mutex.synchronize do
      if @cached_at.nil? || Time.current - @cached_at >= POLL_INTERVAL
        @cached_value = FeatureFlag.boolean_enabled?(FLAG_KEY)
        @cached_at = Time.current
      end
      @cached_value
    end
  end
end
