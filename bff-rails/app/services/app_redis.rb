require "redis"

module AppRedis
  def self.instance
    @instance ||= Redis.new(url: AppConfig.redis_url)
  end
end
