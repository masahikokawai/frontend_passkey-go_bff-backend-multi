require "securerandom"
require "json"

module Auth
  # CONTRACT.mdセクション22: パスキー登録・ログインのchallengeを、begin〜finishの間
  # サーバー側で保持する(Auth::PendingLoginStoreと全く同じ設計、TTL付きRedis保存)。
  # 登録時はuser_idも一緒に保持し、finish時に「登録開始時と同じユーザーか」を確認する
  class PasskeyChallengeStore
    KEY_PREFIX = "bff-rails:passkey-challenge:".freeze
    TTL_SECONDS = 300

    Challenge = Struct.new(:state, :challenge, :user_id, keyword_init: true)

    class << self
      def create(challenge:, user_id: nil)
        state = SecureRandom.hex(16)
        AppRedis.instance.set(
          KEY_PREFIX + state,
          { state: state, challenge: challenge, user_id: user_id }.to_json,
          ex: TTL_SECONDS
        )
        Challenge.new(state: state, challenge: challenge, user_id: user_id)
      end

      # 【セキュリティ監査(3回目)で発見・修正】以前はGET→DELという2つの別々のRedisコマンドで
      # 実装されており、同じstateに対する2つの並行リクエスト(リプレイ攻撃者が同一の
      # WebAuthnアサーションを短時間に2回送りつけるケースを想定)が、どちらも削除前のGETに
      # 成功してしまい、「一度きりのchallenge」という前提が崩れるTOCTOU競合状態があった
      # (bff(Go)側のwebauthn_challenge_store.goにも同種のバグがあり、同じタイミングで発見・修正した)。
      # GETDEL(Redis 6.2+の原子的な単一コマンド、redis gem 5系で`getdel`として提供)に
      # 統一することで、並行呼び出しのうち必ず1つだけが成功するようになる
      def consume(state)
        return nil if state.blank?

        # 【テスト監査で発見・修正】stateはJSONボディ由来のためString以外の型
        # (例: 数値)が来る余地があり、`KEY_PREFIX + state`(String#+)はString以外を
        # 渡されると`TypeError: no implicit conversion of Integer into String`を
        # 送出し未処理のまま500になっていた。文字列補間(常に#to_sされる)に変更し、
        # どんな型が来ても単に「該当キーが無い」として安全に扱われるようにする
        key = "#{KEY_PREFIX}#{state}"
        raw = AppRedis.instance.getdel(key)
        return nil unless raw

        parsed = JSON.parse(raw, symbolize_names: true)
        Challenge.new(**parsed)
      end
    end
  end
end
