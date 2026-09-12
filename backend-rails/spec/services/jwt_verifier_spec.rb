require "rails_helper"

# backend/internal/authjwt/dispatcher_test.go・hmac_test.go・jwks_test.go のRails版
# 3issuer(Keycloak/ローカルHMAC/ローカルRSA)の振り分けと検証を確認する
RSpec.describe JwtVerifier do
  let(:hmac_secret) { "test-hmac-secret" }
  let(:verifier) { described_class.new(local_hmac_secret: hmac_secret, expected_audience: "backend") }

  def hmac_token(iss: JwtVerifier::LOCAL_HMAC_ISSUER, aud: "backend", secret: hmac_secret, sub: "42", exp: 1.hour.from_now)
    JWT.encode({ sub: sub, iss: iss, aud: aud, exp: exp.to_i }, secret, "HS256")
  end

  describe "ローカルHMAC(iss=bff-gin-local-hmac)" do
    it "正しい署名・iss・audなら検証を通しClaimsを返す" do
      claims = verifier.verify(hmac_token)
      expect(claims.subject).to eq("42")
      expect(claims.issuer).to eq(JwtVerifier::LOCAL_HMAC_ISSUER)
      expect(claims.local_issuer?).to be true
    end

    it "シークレットが違うと検証に失敗する" do
      token = hmac_token(secret: "wrong-secret")
      expect { verifier.verify(token) }.to raise_error(JwtVerifier::VerificationError)
    end

    it "audが違うと検証に失敗する" do
      token = hmac_token(aud: "someone-else")
      expect { verifier.verify(token) }.to raise_error(JwtVerifier::VerificationError)
    end

    it "有効期限切れなら検証に失敗する" do
      token = hmac_token(exp: 1.hour.ago)
      expect { verifier.verify(token) }.to raise_error(JwtVerifier::VerificationError)
    end
  end

  describe "ローカルRSA(iss=bff-gin-local-rsa、JWKS経由)" do
    let(:rsa_key) { OpenSSL::PKey::RSA.generate(2048) }
    let(:jwk) { JWT::JWK.new(rsa_key) }

    before do
      jwks_body = { keys: [jwk.export(include_private: false).merge(use: "sig")] }.to_json
      allow(Net::HTTP).to receive(:get).and_return(jwks_body)
    end

    it "JWKS経由でRS256署名を検証しClaimsを返す" do
      token = JWT.encode(
        { sub: "7", iss: JwtVerifier::LOCAL_RSA_ISSUER, aud: "backend", exp: 1.hour.from_now.to_i },
        rsa_key, "RS256", { kid: jwk.kid }
      )
      claims = verifier.verify(token)
      expect(claims.subject).to eq("7")
      expect(claims.local_issuer?).to be true
    end

    it "JWKSに無いkidで署名された(別鍵の)トークンは検証に失敗する" do
      other_key = OpenSSL::PKey::RSA.generate(2048)
      token = JWT.encode(
        { sub: "7", iss: JwtVerifier::LOCAL_RSA_ISSUER, aud: "backend", exp: 1.hour.from_now.to_i },
        other_key, "RS256", { kid: "unknown-kid" }
      )
      expect { verifier.verify(token) }.to raise_error(JwtVerifier::VerificationError)
    end
  end

  describe "不明なissuer" do
    it "Keycloak/ローカルHMAC/ローカルRSAいずれでもないissは検証に失敗する" do
      token = JWT.encode({ sub: "1", iss: "unknown-issuer" }, "x", "HS256")
      expect { verifier.verify(token) }.to raise_error(JwtVerifier::VerificationError)
    end
  end

  describe "不正な形式のトークン" do
    it "JWTとして体を成していない文字列は検証に失敗する" do
      expect { verifier.verify("not-a-jwt") }.to raise_error(JwtVerifier::VerificationError)
    end
  end
end
