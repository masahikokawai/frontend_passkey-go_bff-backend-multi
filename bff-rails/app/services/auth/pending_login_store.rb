require "securerandom"
require "json"
require "digest"
require "base64"

module Auth
  # OIDCのstate/nonceは、認可リクエスト送出からコールバック受信までの間、
  # サーバー側のどこかに保持しておく必要がある(CSRF対策・リプレイ対策)。
  # bff-railsはAPI-onlyでブラウザ側Cookieセッションを持たないため、Redisに
  # stateをキーにして一時保存する(TTL 5分、正規のOIDCフローなら数秒で消費される)。
  #
  # 使用したgem(omniauth-openid-connect / openid_connect)もここに記録しておき、
  # コールバック処理時は「今のFeature Flagの値」ではなく「ログイン開始時点の値」で
  # 分岐する(フロー途中でflagが切り替わっても矛盾なく処理を完了させるため)
  #
  # 【実機検証で判明】Keycloak側の`bff-rails`クライアントは`pkce.code.challenge.method: S256`が
  # 設定されており(bff-gin等と同じ設定を踏襲)、code_challengeが無いと
  # `invalid_request: Missing parameter: code_challenge_method`で認可リクエスト自体が
  # 拒否される。そのためcode_verifier/code_challengeもここで生成・保管する(RFC 7636)
  class PendingLoginStore
    KEY_PREFIX = "bff-rails:pending-login:".freeze
    TTL_SECONDS = 300

    Pending = Struct.new(:state, :nonce, :gem, :code_verifier, keyword_init: true) do
      def code_challenge
        Base64.urlsafe_encode64(Digest::SHA256.digest(code_verifier), padding: false)
      end
    end

    class << self
      def create(gem:)
        state = SecureRandom.hex(16)
        nonce = SecureRandom.hex(16)
        code_verifier = SecureRandom.urlsafe_base64(48)
        AppRedis.instance.set(
          KEY_PREFIX + state,
          { state: state, nonce: nonce, gem: gem, code_verifier: code_verifier }.to_json,
          ex: TTL_SECONDS
        )
        Pending.new(state: state, nonce: nonce, gem: gem, code_verifier: code_verifier)
      end

      # 【セキュリティ監査(3回目)で発見・修正、Auth::PasskeyChallengeStoreと同じ理由】
      # GET→DELの2コマンドはTOCTOU競合状態を生む(同じstateへの並行リクエストが両方
      # 消費に成功しうる = OIDCコールバックのリプレイに対する耐性が弱くなる)。
      # GETDEL(原子的な単一コマンド)に統一する
      def consume(state)
        return nil if state.blank?

        key = KEY_PREFIX + state
        raw = AppRedis.instance.getdel(key)
        return nil unless raw

        parsed = JSON.parse(raw, symbolize_names: true)
        Pending.new(**parsed)
      end
    end
  end
end
