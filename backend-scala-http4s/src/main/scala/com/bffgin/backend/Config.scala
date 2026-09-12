package com.bffgin.backend

// backend/internal/config/config.go の環境変数命名規則をそのまま踏襲する
// (同じdocker-compose環境にGo実装と同時に立てて比較できるように、既定値も完全に揃える)
final case class Config(
    httpAddr: String,
    grpcAddr: String,
    dbDsn: String,
    keycloakIssuer: String,
    keycloakJwksUrl: String,
    expectedAudience: String,
    localHmacSecret: String,
    localHmacIssuer: String,
    localRsaJwksUrl: String,
    localRsaIssuer: String,
    // CONTRACT.mdセクション11・20.7: 外部公開API(BFF非経由、Client Credentials Grant)
    externalHttpAddr: String,
    externalApiClientId: String
)

object Config {
  private def env(key: String, default: String): String =
    sys.env.getOrElse(key, default)

  def load(): Config = {
    val keycloakIssuer = env("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training")
    Config(
      httpAddr = env("HTTP_ADDR", ":8094"),
      grpcAddr = env("GRPC_ADDR", ":9094"),
      dbDsn = env("DB_DSN", "root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true"),
      keycloakIssuer = keycloakIssuer,
      keycloakJwksUrl = env("KEYCLOAK_JWKS_URL", keycloakIssuer + "/protocol/openid-connect/certs"),
      expectedAudience = env("EXPECTED_AUDIENCE", "backend"),
      localHmacSecret = env("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me"),
      localHmacIssuer = "bff-gin-local-hmac",
      localRsaJwksUrl = env("LOCAL_AUTH_RSA_JWKS_URL", "http://localhost:8080/.well-known/jwks.json"),
      localRsaIssuer = "bff-gin-local-rsa",
      externalHttpAddr = env("EXTERNAL_HTTP_ADDR", ":8099"),
      externalApiClientId = env("EXTERNAL_API_CLIENT_ID", "external-api-client")
    )
  }

  /** GoのDB_DSN形式("user:pass@tcp(host:port)/dbname?params")をJDBC URLへ変換する
    * 既定値は"root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true"のように
    * パスワード無しの場合があるため、その形も扱う
    */
  def toJdbcUrl(dsn: String): (String, String, String) = {
    val userInfoAndRest = dsn.split("@tcp\\(", 2)
    val userInfo = userInfoAndRest(0)
    val rest = userInfoAndRest(1) // "host:port)/dbname?params"
    val hostPortAndRest = rest.split("\\)/", 2)
    val hostPort = hostPortAndRest(0)
    val dbNameAndParams = hostPortAndRest(1)
    val dbName = dbNameAndParams.split("\\?", 2)(0)

    val (user, pass) = userInfo.split(":", 2) match {
      case Array(u, p) => (u, p)
      case Array(u)    => (u, "")
    }
    val jdbcUrl = s"jdbc:mysql://$hostPort/$dbName?useSSL=false&allowPublicKeyRetrieval=true&serverTimezone=UTC"
    (jdbcUrl, user, pass)
  }
}
