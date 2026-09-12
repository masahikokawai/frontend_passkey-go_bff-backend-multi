//! backend(Go)の internal/config/config.go と環境変数名・既定値をそろえている
//! 同じdocker-compose環境の上で両方を同時に起動して比較できるようにするため

#[derive(Debug, Clone)]
pub struct Config {
    /// REST(Axum)の待受アドレス
    /// Go実装(:8090)・gRPCポート(9093)と衝突しない値として:8093を既定にした
    pub http_addr: String,
    /// gRPC(tonic)の待受アドレス
    pub grpc_addr: String,
    /// 外部公開API(CONTRACT.mdセクション11)の待受アドレス
    /// 内部REST(:8093)・gRPC(:9093)とは別
    /// Go実装の内部アドレス(:8097)・gateway(:8081)とも衝突しない値として:8098を既定にした
    pub external_http_addr: String,
    /// Client Credentials Grantで発行されたトークンのazp(authorized party)クレームと比較する期待値
    /// backend(Go)のExternalAPIClientIDと同じ既定値
    pub external_api_client_id: String,
    /// `backend.external-tasks-pagination-v2`のポーリング間隔(秒)
    /// backend(Go)の FeatureFlagPollInterval既定値(10秒)と合わせる
    pub feature_flag_poll_interval_secs: u64,

    pub db_dsn: String,

    pub keycloak_issuer: String,
    pub keycloak_jwks_url: String,
    pub expected_audience: String,

    pub local_hmac_secret: String,
    pub local_rsa_jwks_url: String,

    pub log_level: String,
}

fn env_default(key: &str, default: &str) -> String {
    std::env::var(key).unwrap_or_else(|_| default.to_string())
}

impl Config {
    pub fn load() -> Self {
        let keycloak_issuer = env_default("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training");
        let keycloak_jwks_url = std::env::var("KEYCLOAK_JWKS_URL")
            .unwrap_or_else(|_| format!("{}/protocol/openid-connect/certs", keycloak_issuer));

        Config {
            http_addr: env_default("HTTP_ADDR", ":8093"),
            grpc_addr: env_default("GRPC_ADDR", ":9093"),
            external_http_addr: env_default("EXTERNAL_HTTP_ADDR", ":8098"),
            external_api_client_id: env_default("EXTERNAL_API_CLIENT_ID", "external-api-client"),
            feature_flag_poll_interval_secs: env_default("FEATURE_FLAG_POLL_INTERVAL_SECONDS", "10")
                .parse()
                .unwrap_or(10),
            db_dsn: env_default(
                "DB_DSN",
                "mysql://root@127.0.0.1:13306/bff_gin_development",
            ),
            keycloak_issuer,
            keycloak_jwks_url,
            expected_audience: env_default("EXPECTED_AUDIENCE", "backend"),
            local_hmac_secret: env_default(
                "LOCAL_AUTH_HMAC_SECRET",
                "local-dev-hmac-shared-secret-change-me",
            ),
            local_rsa_jwks_url: env_default(
                "LOCAL_AUTH_RSA_JWKS_URL",
                "http://localhost:8080/.well-known/jwks.json",
            ),
            log_level: env_default("LOG_LEVEL", "info"),
        }
    }
}
