require "jwt"
require "net/http"
require "json"

# JwtVerifier は backend/internal/authjwt (Go) の Dispatcher/Verifier/HMACVerifier を
# Rubyへ移植したもの(CONTRACT.mdセクション20.5: ワイヤー契約パリティの原則)
#
# 3つの発行元(iss)を検証する:
#   - Keycloak(RS256、JWKS、iss=KEYCLOAK_ISSUER)
#   - ローカルHMAC(HS256、共有シークレット、iss="bff-gin-local-hmac")
#   - ローカルRSA(RS256、bffが公開するJWKS、iss="bff-gin-local-rsa")
#
# 検証に失敗した場合は JwtVerifier::VerificationError を投げる
# 呼び出し側(TasksController/gRPC TaskService)がこれを捕捉して401/unauthenticatedへ変換する
class JwtVerifier
  class VerificationError < StandardError; end

  LOCAL_HMAC_ISSUER = "bff-gin-local-hmac".freeze
  LOCAL_RSA_ISSUER = "bff-gin-local-rsa".freeze

  # azp(authorized party)は外部公開API(CONTRACT.mdセクション11)専用の追加チェックにのみ使う
  # 内部CRUD(HMAC/RSA/Keycloakいずれのuser向けトークン)ではazpクレーム自体が
  # 無いことも多く、その場合はnilのまま(呼び出し側は外部APIのcontrollerだけがこれを見る)
  Claims = Struct.new(:subject, :issuer, :azp, keyword_init: true) do
    def local_issuer?
      issuer == LOCAL_HMAC_ISSUER || issuer == LOCAL_RSA_ISSUER
    end
  end

  def initialize(
    keycloak_issuer: ENV.fetch("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training"),
    keycloak_jwks_url: ENV["KEYCLOAK_JWKS_URL"],
    expected_audience: ENV.fetch("EXPECTED_AUDIENCE", "backend"),
    local_hmac_secret: ENV.fetch("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me"),
    local_rsa_jwks_url: ENV.fetch("LOCAL_AUTH_RSA_JWKS_URL", "http://localhost:8080/.well-known/jwks.json")
  )
    @keycloak_issuer = keycloak_issuer
    @keycloak_jwks_url = keycloak_jwks_url.presence || "#{keycloak_issuer}/protocol/openid-connect/certs"
    @expected_audience = expected_audience
    @local_hmac_secret = local_hmac_secret
    @local_rsa_jwks_url = local_rsa_jwks_url

    @jwks_cache = {} # url => JWT::JWK::Set
  end

  # token_string を検証し、Claims を返す
  # 失敗時は VerificationError を投げる
  def verify(token_string)
    issuer = peek_issuer(token_string)

    case issuer
    when @keycloak_issuer
      verify_rs256(token_string, jwks_url: @keycloak_jwks_url, issuer: issuer)
    when LOCAL_HMAC_ISSUER
      verify_hs256(token_string, issuer: issuer)
    when LOCAL_RSA_ISSUER
      verify_rs256(token_string, jwks_url: @local_rsa_jwks_url, issuer: issuer)
    else
      raise VerificationError, "不明なissuer: #{issuer.inspect}"
    end
  rescue JWT::DecodeError => e
    raise VerificationError, "JWT検証失敗: #{e.message}"
  end

  private

  # iss確認前の「覗き見」
  # Dispatcher(Go)のParseUnverifiedと同じ位置づけで、
  # ここで得たissは署名検証前の値のため、実際の信頼は各verifierの署名検証に委ねる
  def peek_issuer(token_string)
    _, payload, _ = token_string.split(".")
    raise VerificationError, "JWTの形式が不正" if payload.nil?

    JSON.parse(Base64.urlsafe_decode64(pad_base64(payload)))["iss"]
  rescue JSON::ParserError, ArgumentError => e
    raise VerificationError, "JWTのパースに失敗(iss確認前): #{e.message}"
  end

  def pad_base64(str)
    str + ("=" * ((4 - str.length % 4) % 4))
  end

  def verify_hs256(token_string, issuer:)
    payload, = JWT.decode(
      token_string, @local_hmac_secret, true,
      algorithm: "HS256", iss: issuer, verify_iss: true,
      aud: @expected_audience, verify_aud: true
    )
    Claims.new(subject: payload["sub"], issuer: payload["iss"], azp: payload["azp"])
  end

  def verify_rs256(token_string, jwks_url:, issuer:)
    jwks_loader = lambda do |options|
      refresh_jwks(jwks_url) if options[:invalidate] || !@jwks_cache[jwks_url]
      @jwks_cache[jwks_url]
    end

    payload, = JWT.decode(
      token_string, nil, true,
      algorithms: ["RS256"], iss: issuer, verify_iss: true,
      aud: @expected_audience, verify_aud: true,
      jwks: jwks_loader
    )
    Claims.new(subject: payload["sub"], issuer: payload["iss"], azp: payload["azp"])
  end

  # kid不一致時のみ再取得する(Go実装のVerifier.refreshと同じキャッシュ戦略)
  def refresh_jwks(url)
    body = Net::HTTP.get(URI.parse(url))
    @jwks_cache[url] = JWT::JWK::Set.new(JSON.parse(body))
  end
end
