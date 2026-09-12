require "rails_helper"

RSpec.describe Auth::PendingLoginStore do
  it "state/nonce/code_verifier/gemを保存し、stateで一度だけ取り出せる(消費後は消える)" do
    pending = described_class.create(gem: "openid_connect")

    found = described_class.consume(pending.state)
    expect(found.state).to eq(pending.state)
    expect(found.nonce).to eq(pending.nonce)
    expect(found.gem).to eq("openid_connect")
    expect(found.code_verifier).to eq(pending.code_verifier)

    expect(described_class.consume(pending.state)).to be_nil
  end

  it "code_challengeはcode_verifierのSHA256をbase64url(パディング無し)したものになる(RFC 7636)" do
    pending = described_class.create(gem: "omniauth-openid-connect")
    expected = Base64.urlsafe_encode64(Digest::SHA256.digest(pending.code_verifier), padding: false)

    expect(pending.code_challenge).to eq(expected)
  end

  it "存在しないstateはnilを返す" do
    expect(described_class.consume("no-such-state")).to be_nil
  end

  # 【セキュリティ監査(3回目)で発見した回帰テスト】Auth::PasskeyChallengeStoreと同じ
  # TOCTOU競合状態がここにもあった(OIDCコールバックのstateが並行リクエストで
  # 二重に消費できてしまう=リプレイに対する耐性が弱くなる)。GETDELへの統一により
  # 大量の並行呼び出しのうち必ず1つだけが成功することを確認する
  it "同一stateへの大量の並行consumeは、1回だけ成功する(リプレイ防止)" do
    pending_login = described_class.create(gem: "openid_connect")

    results = Array.new(200) do
      Thread.new { described_class.consume(pending_login.state) }
    end.map(&:value)

    succeeded = results.count { |r| !r.nil? }
    expect(succeeded).to eq(1)
  end
end
