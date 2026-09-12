require "rails_helper"

RSpec.describe SessionStore do
  it "セッションを保存・取得・削除できる" do
    session = SessionStore::Session.new(
      keycloak_sub: "sub-1", name: "Taro", email: "taro@example.com",
      access_token: "at", refresh_token: "rt"
    )
    id = described_class.create(session)

    found = described_class.find(id)
    expect(found.name).to eq("Taro")
    expect(found.access_token).to eq("at")

    described_class.destroy(id)
    expect(described_class.find(id)).to be_nil
  end

  it "存在しないIDやblankはnilを返す" do
    expect(described_class.find(nil)).to be_nil
    expect(described_class.find("does-not-exist")).to be_nil
  end

  # 【2回目のテスト監査で追加】token refresh後、同じsession_id(=同じCookie値)のまま
  # 中身だけ差し替えられること、かつTTLがリセットされず維持されること(既存セッションの
  # 残り有効期限を、リフレッシュの度に延長しない設計であることの確認)
  describe ".update" do
    it "同じidのまま中身を更新できる(TTLは維持される)" do
      session = SessionStore::Session.new(
        keycloak_sub: "s", name: "n", email: "e", access_token: "old-at", refresh_token: "old-rt", auth_mode: "keycloak"
      )
      id = described_class.create(session)
      original_ttl = AppRedis.instance.ttl(SessionStore::KEY_PREFIX + id)

      session.access_token = "new-at"
      session.refresh_token = "new-rt"
      described_class.update(id, session)

      found = described_class.find(id)
      expect(found.access_token).to eq("new-at")
      expect(found.refresh_token).to eq("new-rt")
      expect(AppRedis.instance.ttl(SessionStore::KEY_PREFIX + id)).to be <= original_ttl
      expect(AppRedis.instance.ttl(SessionStore::KEY_PREFIX + id)).to be > 0
    end
  end
end
