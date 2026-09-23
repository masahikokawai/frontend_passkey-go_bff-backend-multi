package com.bffgin.backend;

/**
 * backend-rustのsrc/config.rs・backend-c/backend-cppのenv var名・既定値と揃えている。
 * 同じdocker-compose環境の上で比較実行できるようにするため
 */
public record Config(
        String httpAddr,
        String grpcAddr,
        String externalHttpAddr,
        String dbHost,
        int dbPort,
        String dbUser,
        String dbPassword,
        String dbSchema,
        String keycloakIssuer,
        String keycloakJwksUrl,
        String expectedAudience,
        String localHmacSecret,
        String localRsaJwksUrl,
        String externalApiClientId,
        String logLevel) {

    public static Config fromEnv() {
        String keycloakIssuer = envOr("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training");
        String keycloakJwksUrl = System.getenv("KEYCLOAK_JWKS_URL");
        if (keycloakJwksUrl == null || keycloakJwksUrl.isEmpty()) {
            keycloakJwksUrl = keycloakIssuer + "/protocol/openid-connect/certs";
        }
        return new Config(
                envOr("HTTP_ADDR", "8111"),
                envOr("GRPC_ADDR", "9101"),
                envOr("EXTERNAL_HTTP_ADDR", "8112"),
                envOr("DB_HOST", "127.0.0.1"),
                Integer.parseInt(envOr("DB_PORT", "13306")),
                envOr("DB_USER", "root"),
                envOr("DB_PASSWORD", ""),
                envOr("DB_SCHEMA", "bff_gin_development"),
                keycloakIssuer,
                keycloakJwksUrl,
                envOr("EXPECTED_AUDIENCE", "backend"),
                envOr("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me"),
                envOr("LOCAL_AUTH_RSA_JWKS_URL", "http://localhost:8080/.well-known/jwks.json"),
                envOr("EXTERNAL_API_CLIENT_ID", "external-api-client"),
                envOr("LOG_LEVEL", "info"));
    }

    private static String envOr(String key, String fallback) {
        String v = System.getenv(key);
        return (v == null || v.isEmpty()) ? fallback : v;
    }

    public String jdbcUrl() {
        return "jdbc:mysql://" + dbHost + ":" + dbPort + "/" + dbSchema
                + "?useSSL=false&allowPublicKeyRetrieval=true&serverTimezone=UTC";
    }
}
