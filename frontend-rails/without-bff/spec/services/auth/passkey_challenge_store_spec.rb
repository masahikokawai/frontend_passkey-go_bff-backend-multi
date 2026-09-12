require "rails_helper"

# CONTRACT.mdセクション22.9: Auth::PasskeyChallengeStoreはRedisが無いこのアプリのための
# Mutex保護プロセス内Hashで、bff(Go)/bff-railsのRedis GETDELと同じ「一度きりのchallenge」を実現する設計。
#
# 【テスト監査で追記】既存のpasskeys_spec.rbには「同じstateでlogin/finishを2回呼ぶ」テストがあるが、
# これは順番に2回呼んでいるだけで、実際に複数スレッドが同時にconsumeを呼んだ場合に
# Mutexが機能しているか(TOCTOU競合が本当に起きないか)までは検証していなかった。
# ここでは実際にThreadを複数立てて競合させ、ちょうど1つだけが成功することを直接確認する
RSpec.describe Auth::PasskeyChallengeStore do
  describe ".consume の並行呼び出し" do
    it "同じstateへ多数のスレッドから同時にconsumeしても、成功するのはちょうど1つだけ" do
      challenge = described_class.create(challenge: "challenge-bytes")
      thread_count = 20
      results = Array.new(thread_count)

      # 【重要】単純に「先にThread.newしたものが先に実行される」わけではないため、
      # Queueで一斉にスタートさせて実際の同時アクセスを起こす
      start_gate = Queue.new
      threads = Array.new(thread_count) do |i|
        Thread.new do
          start_gate.pop
          results[i] = described_class.consume(challenge.state)
        end
      end
      thread_count.times { start_gate << true }
      threads.each(&:join)

      successes = results.compact
      expect(successes.size).to eq(1)
      expect(successes.first.state).to eq(challenge.state)
    end

    it "異なるstateへの並行consumeは、それぞれ独立して成功する(過剰な排他になっていない)" do
      challenges = Array.new(10) { described_class.create(challenge: "c") }
      start_gate = Queue.new
      results = Array.new(10)
      threads = challenges.each_with_index.map do |c, i|
        Thread.new do
          start_gate.pop
          results[i] = described_class.consume(c.state)
        end
      end
      10.times { start_gate << true }
      threads.each(&:join)

      expect(results.compact.size).to eq(10)
    end
  end

  describe ".consume の基本動作" do
    it "登録したuser_idをそのまま返す" do
      challenge = described_class.create(challenge: "c", user_id: 42)
      consumed = described_class.consume(challenge.state)
      expect(consumed.user_id).to eq(42)
    end

    it "存在しないstateはnilを返す" do
      expect(described_class.consume("does-not-exist")).to be_nil
    end

    it "blankなstateはnilを返す" do
      expect(described_class.consume(nil)).to be_nil
      expect(described_class.consume("")).to be_nil
    end
  end
end
