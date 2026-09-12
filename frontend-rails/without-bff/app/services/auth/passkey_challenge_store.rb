require "securerandom"

module Auth
  # CONTRACT.mdセクション22.9: パスキー登録・ログインのchallengeを、begin〜finishの間
  # サーバー側で保持する。
  #
  # bff(Go)・bff-railsはRedisを使うが、frontend-rails/without-bffにはRedisが無い
  # (セクション21.4、意図的に依存を持たない設計)。今回のスコープに対してRedis新規導入は
  # 過剰と判断し、代わりにMutexで保護したプロセス内Hashで同等の「一度きりのchallenge」を実現する。
  #
  # 【3回目のセキュリティ監査で見つかったbff(Go)/bff-rails側の同種バグを踏まえた設計】
  # consumeを「読み取り→削除」の2ステップに分けると、同一stateへの2並行リクエストが
  # 両方とも削除前の読み取りに成功してしまうTOCTOU競合状態になる(bff-gin/webauthn_challenge_store.go・
  # bff-rails/passkey_challenge_store.rbで実際に発見・修正済み)。ここでも`Mutex#synchronize`の
  # 中で`Hash#delete`(読み取りと削除が1つの操作)を行うことで、並行呼び出しのうち必ず1つだけが
  # 成功するようにしている(Redisの`GETDEL`と同じ効果を、プロセス内Mutexで実現する)。
  #
  # 【既知の制約】この排他制御はプロセス内でのみ有効。config/puma.rbはworker数を指定しておらず
  # (既定1プロセス)前提が成立しているが、複数workerプロセス構成に変える場合はRedis等の
  # プロセス外ストアへの切り替えが必要になる。
  class PasskeyChallengeStore
    TTL_SECONDS = 300

    Challenge = Struct.new(:state, :challenge, :user_id, keyword_init: true)

    @mutex = Mutex.new
    @entries = {}

    class << self
      def create(challenge:, user_id: nil)
        state = SecureRandom.hex(16)
        entry = { challenge: challenge, user_id: user_id, expires_at: Process.clock_gettime(Process::CLOCK_MONOTONIC) + TTL_SECONDS }
        @mutex.synchronize { @entries[state] = entry }
        Challenge.new(state: state, challenge: challenge, user_id: user_id)
      end

      def consume(state)
        return nil if state.blank?

        entry = @mutex.synchronize { @entries.delete(state) }
        return nil if entry.nil?
        return nil if entry[:expires_at] < Process.clock_gettime(Process::CLOCK_MONOTONIC)

        Challenge.new(state: state, challenge: entry[:challenge], user_id: entry[:user_id])
      end
    end
  end
end
