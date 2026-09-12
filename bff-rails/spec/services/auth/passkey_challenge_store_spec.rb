require "rails_helper"

RSpec.describe Auth::PasskeyChallengeStore do
  it "challengeを保存・取得でき、1度取得すると消える" do
    created = described_class.create(challenge: "chal-123", user_id: 9)
    found = described_class.consume(created.state)

    expect(found.challenge).to eq("chal-123")
    expect(found.user_id).to eq(9)
    expect(described_class.consume(created.state)).to be_nil
  end

  it "user_id省略時(ログイン用)はnilのまま保存できる" do
    created = described_class.create(challenge: "chal-456")
    found = described_class.consume(created.state)
    expect(found.user_id).to be_nil
  end

  it "存在しないstateやblankはnilを返す" do
    expect(described_class.consume(nil)).to be_nil
    expect(described_class.consume("does-not-exist")).to be_nil
  end

  # 【セキュリティ監査(3回目)で発見した回帰テスト】以前はconsumeがGET→DELという
  # 2つの別々のRedisコマンドで実装されており、同じstateへの並行リクエスト
  # (リプレイ攻撃者が同一のWebAuthnアサーションを短時間に2回送りつけるケースを想定)が
  # どちらも削除前のGETに成功してしまい、「一度きりのchallenge」という前提が崩れる
  # TOCTOU競合状態があった。GETDEL(Redisの原子的な単一コマンド)への統一により、
  # 大量の並行呼び出しのうち必ず1つだけが成功することを確認する
  # (bff(Go)側のwebauthn_challenge_store_test.goと同じ検証方針)
  it "同一stateへの大量の並行consumeは、1回だけ成功する(リプレイ防止)" do
    created = described_class.create(challenge: "chal-race", user_id: 1)

    results = Array.new(200) do
      Thread.new { described_class.consume(created.state) }
    end.map(&:value)

    succeeded = results.count { |r| !r.nil? }
    expect(succeeded).to eq(1)
  end
end
