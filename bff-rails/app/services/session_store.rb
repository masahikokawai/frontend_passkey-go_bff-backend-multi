require "securerandom"
require "json"

# bff(Go)のinternal/auth/session.goと同じ役割: ブラウザにはCookieという不透明な鍵しか渡さず、
# 実データ(access_token等)はRedisにだけ持つ(BFFパターンの核心)
class SessionStore
  KEY_PREFIX = "bff-rails:session:".freeze
  TTL_SECONDS = 1800

  # auth_mode: "keycloak"(OIDC経由)または"passkey"(CONTRACT.mdセクション22、Keycloak非経由)
  Session = Struct.new(:user_id, :keycloak_sub, :name, :email, :access_token, :refresh_token, :auth_mode,
                       keyword_init: true) do
    def to_json_string
      to_h.to_json
    end
  end

  class << self
    def create(session)
      id = SecureRandom.hex(32)
      AppRedis.instance.set(KEY_PREFIX + id, session.to_json_string, ex: TTL_SECONDS)
      id
    end

    # 【2回目のテスト監査で追加】token refresh成功時、同じsession_id(=同じCookie値)のまま
    # 中身(access_token等)だけ差し替えるために使う。TTLは既存のセッション寿命をリセットせず
    # そのまま(残り時間を維持)にする方が自然なため、TTLは指定しない
    # (Redisの`SET`にexオプションを付けなければ既存TTLは保持される、という挙動に依拠する)
    def update(id, session)
      AppRedis.instance.set(KEY_PREFIX + id, session.to_json_string, keepttl: true)
    end

    def find(id)
      return nil if id.blank?

      raw = AppRedis.instance.get(KEY_PREFIX + id)
      return nil unless raw

      Session.new(JSON.parse(raw, symbolize_names: true))
    end

    def destroy(id)
      AppRedis.instance.del(KEY_PREFIX + id) if id.present?
    end
  end
end
