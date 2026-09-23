package com.bffgin.backend

/**
 * backend-java/backend-rustのenv var名・既定値と揃えている
 * (同じdocker-compose環境の上で比較実行できるようにするため)
 */
data class Config(
    val httpAddr: String,
    val grpcAddr: String,
    val externalHttpAddr: String,
    val dbHost: String,
    val dbPort: Int,
    val dbUser: String,
    val dbPassword: String,
    val dbSchema: String,
    val keycloakIssuer: String,
    val keycloakJwksUrl: String,
    val expectedAudience: String,
    val localHmacSecret: String,
    val localRsaJwksUrl: String,
    val externalApiClientId: String,
) {
    val jdbcUrl: String
        get() = "jdbc:mysql://$dbHost:$dbPort/$dbSchema?useSSL=false&allowPublicKeyRetrieval=true&serverTimezone=UTC"

    companion object {
        fun fromEnv(): Config {
            val keycloakIssuer = envOr("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training")
            val keycloakJwksUrl = System.getenv("KEYCLOAK_JWKS_URL")?.ifEmpty { null }
                ?: "$keycloakIssuer/protocol/openid-connect/certs"
            return Config(
                httpAddr = envOr("HTTP_ADDR", "8113"),
                grpcAddr = envOr("GRPC_ADDR", "9102"),
                externalHttpAddr = envOr("EXTERNAL_HTTP_ADDR", "8114"),
                dbHost = envOr("DB_HOST", "127.0.0.1"),
                dbPort = envOr("DB_PORT", "13306").toInt(),
                dbUser = envOr("DB_USER", "root"),
                dbPassword = envOr("DB_PASSWORD", ""),
                dbSchema = envOr("DB_SCHEMA", "bff_gin_development"),
                keycloakIssuer = keycloakIssuer,
                keycloakJwksUrl = keycloakJwksUrl,
                expectedAudience = envOr("EXPECTED_AUDIENCE", "backend"),
                localHmacSecret = envOr("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me"),
                localRsaJwksUrl = envOr("LOCAL_AUTH_RSA_JWKS_URL", "http://localhost:8080/.well-known/jwks.json"),
                externalApiClientId = envOr("EXTERNAL_API_CLIENT_ID", "external-api-client"),
            )
        }

        private fun envOr(key: String, fallback: String): String {
            val v = System.getenv(key)
            return if (v.isNullOrEmpty()) fallback else v
        }
    }
}
