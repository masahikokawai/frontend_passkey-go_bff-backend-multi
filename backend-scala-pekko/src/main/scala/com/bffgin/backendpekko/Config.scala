package com.bffgin.backendpekko

// backend/internal/config/config.go と同じ環境変数の命名規則・既定値を踏襲する
// (同じdocker-compose環境にGo実装と同時に立てて比較できるようにするため)
final case class Config(
    httpAddr: String,
    grpcAddr: String,
    dbDsnHost: String,
    dbDsnPort: Int,
    dbDsnDatabase: String,
    keycloakIssuer: String,
    keycloakJwksUrl: String,
    expectedAudience: String,
    localHmacSecret: String,
    localRsaJwksUrl: String,
    logLevel: String,
    // CONTRACT.mdセクション11・20.7: bff非経由の外部公開API(Client Credentials Grant)
    // Goの:8097相当
    //
    // 5言語で外部公開APIを持つのはGoのみのため、この:8100はゲートウェイからは
    // 直接使われない(ゲートウェイは常にGoの:8097へフォールバックする)が、単体で動作確認できるよう用意する
    externalHttpAddr: String,
    externalApiClientId: String
)

object Config {
  def load(): Config = {
    val keycloakIssuer = env("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training")
    Config(
      httpAddr = env("HTTP_ADDR", ":8095"),
      grpcAddr = env("GRPC_ADDR", ":9095"),
      dbDsnHost = env("DB_HOST", "127.0.0.1"),
      dbDsnPort = env("DB_PORT", "13306").toInt,
      dbDsnDatabase = env("DB_NAME", "bff_gin_development"),
      keycloakIssuer = keycloakIssuer,
      keycloakJwksUrl = envOpt("KEYCLOAK_JWKS_URL").getOrElse(s"$keycloakIssuer/protocol/openid-connect/certs"),
      expectedAudience = env("EXPECTED_AUDIENCE", "backend"),
      localHmacSecret = env("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me"),
      localRsaJwksUrl = env("LOCAL_AUTH_RSA_JWKS_URL", "http://localhost:8080/.well-known/jwks.json"),
      logLevel = env("LOG_LEVEL", "info"),
      externalHttpAddr = env("EXTERNAL_HTTP_ADDR", ":8100"),
      externalApiClientId = env("EXTERNAL_API_CLIENT_ID", "external-api-client")
    )
  }

  private def env(key: String, default: String): String =
    sys.env.getOrElse(key, default)

  private def envOpt(key: String): Option[String] =
    sys.env.get(key).filter(_.nonEmpty)
}
