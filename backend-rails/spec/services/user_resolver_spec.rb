require "rails_helper"

# backend/internal/handler/v1/task.go の resolveUserID(CONTRACT.mdセクション16.5)のRails版。
RSpec.describe UserResolver do
  let!(:user) { User.create!(keycloak_sub: "kc-sub-1", email: "u@example.com", name: "U", role: 1) }

  it "ローカル発行issのsubは内部user_idそのものとして解決する" do
    claims = JwtVerifier::Claims.new(subject: user.id.to_s, issuer: JwtVerifier::LOCAL_HMAC_ISSUER)
    expect(described_class.resolve(claims)).to eq(user)
  end

  it "ローカル発行issでも存在しないuser_idならUserNotProvisioned" do
    claims = JwtVerifier::Claims.new(subject: "999999", issuer: JwtVerifier::LOCAL_HMAC_ISSUER)
    expect { described_class.resolve(claims) }.to raise_error(UserResolver::UserNotProvisioned)
  end

  it "ローカル発行issでsubが数値でなければUserNotProvisioned" do
    claims = JwtVerifier::Claims.new(subject: "not-a-number", issuer: JwtVerifier::LOCAL_HMAC_ISSUER)
    expect { described_class.resolve(claims) }.to raise_error(UserResolver::UserNotProvisioned)
  end

  it "Keycloak発行issのsubはkeycloak_subとして解決する" do
    claims = JwtVerifier::Claims.new(subject: "kc-sub-1", issuer: "http://localhost:8082/realms/training")
    expect(described_class.resolve(claims)).to eq(user)
  end

  it "Keycloak発行issでkeycloak_subが一致しなければUserNotProvisioned(削除済みユーザーのケース)" do
    claims = JwtVerifier::Claims.new(subject: "kc-sub-deleted", issuer: "http://localhost:8082/realms/training")
    expect { described_class.resolve(claims) }.to raise_error(UserResolver::UserNotProvisioned)
  end

  # 【セキュリティ監査で追加】UserResolver.resolveはclaims.azpを一切参照していない。
  # そのため外部公開API用のClient Credentials Grantトークン(external-api-clientが取得する、
  # aud=backendが注入されたトークン)は、署名検証・issuer検証等の「認証」自体は
  # 内部API(backend-rails/app/controllers/internal/v1/tasks_controller.rb等)も
  # 通過してしまう。これが安全なのは、external-api-client自身のサービスアカウントsub
  # (Keycloakの慣例で"service-account-external-api-client"のような形式になる)が、
  # JITプロビジョニング(通常ユーザーのログイン時のみ実行される)を一度も経ておらず
  # usersテーブルに該当行が存在しないため、上の「keycloak_subが一致しなければ
  # UserNotProvisioned」と全く同じ経路で必ず拒否されるという「2段構えの安全性」に
  # 依存しているからである。このテストはその安全性を明示的に固定する
  # (Go・Rust・Scala(http4s)・Scala(Pekko)の各実装にも同じ観点のテストを追加済み)。
  # azpに実際の値を設定しても判定に一切影響しない(subだけで拒否される)ことも確認する。
  it "外部公開API用トークン(external-api-clientのサービスアカウント)は内部APIのuser解決に失敗する" do
    claims = JwtVerifier::Claims.new(
      subject: "service-account-external-api-client",
      issuer: "http://localhost:8082/realms/training",
      azp: "external-api-client"
    )
    expect { described_class.resolve(claims) }.to raise_error(UserResolver::UserNotProvisioned)
  end
end
