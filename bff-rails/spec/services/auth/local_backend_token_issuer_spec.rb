require "rails_helper"
require "jwt"

RSpec.describe Auth::LocalBackendTokenIssuer do
  describe ".issue" do
    it "bff(Go)のローカルHMAC認証と同じクレーム形状(iss/sub/aud/name/email/roles)で発行する" do
      token = described_class.issue(user_id: 42, name: "太郎", email: "taro@example.com", roles: ["general"])

      decoded = JWT.decode(token, AppConfig.local_auth_hmac_secret, true, algorithm: "HS256")
      claims = decoded.first

      expect(claims["iss"]).to eq("bff-gin-local-hmac")
      expect(claims["sub"]).to eq("42")
      expect(claims["aud"]).to eq(["backend"])
      expect(claims["name"]).to eq("太郎")
      expect(claims["email"]).to eq("taro@example.com")
      expect(claims["roles"]).to eq(["general"])
      expect(claims["exp"] - claims["iat"]).to eq(24 * 60 * 60)
    end

    it "roles未指定(nil)でも空配列として扱い落ちない" do
      token = described_class.issue(user_id: 1, name: nil, email: nil, roles: nil)
      claims = JWT.decode(token, AppConfig.local_auth_hmac_secret, true, algorithm: "HS256").first
      expect(claims["roles"]).to eq([])
      expect(claims["name"]).to eq("")
      expect(claims["email"]).to eq("")
    end

    it "誤ったシークレットでは検証に失敗する(署名が共有シークレットに依存していることの確認)" do
      token = described_class.issue(user_id: 1, name: "x", email: "x@example.com", roles: [])
      expect {
        JWT.decode(token, "wrong-secret", true, algorithm: "HS256")
      }.to raise_error(JWT::VerificationError)
    end
  end
end
