'use strict';

// backend(Go)の internal/config/config.go と環境変数名・既定値をそろえている
// 同じdocker-compose環境の上で他言語実装と同時に起動して比較できるようにするため

function envDefault(key, fallback) {
  const v = process.env[key];
  return v === undefined || v === '' ? fallback : v;
}

function loadConfig() {
  const keycloakIssuer = envDefault('KEYCLOAK_ISSUER', 'http://localhost:8082/realms/training');
  const keycloakJwksUrl = envDefault(
    'KEYCLOAK_JWKS_URL',
    `${keycloakIssuer}/protocol/openid-connect/certs`,
  );

  return {
    // REST(Express)の待受アドレス
    // 他言語(Go:8090、Rust:8093、Scala(http4s):8094、Scala(Pekko):8095、Rails:8096)と
    // 衝突しない値として:8103を既定にした
    httpAddr: envDefault('HTTP_ADDR', ':8103'),
    // gRPC(@grpc/grpc-js)の待受アドレス
    grpcAddr: envDefault('GRPC_ADDR', ':9097'),
    // 外部公開API(CONTRACT.mdセクション11)の待受アドレス
    // gateway(:8081)・他言語の外部アドレス(Go:8097、Rust:8098、http4s:8099、Pekko:8100、Rails:8101)
    // ・bff-rails(:8102)と衝突しない値として:8107を既定にした
    externalHttpAddr: envDefault('EXTERNAL_HTTP_ADDR', ':8107'),
    // Client Credentials Grantで発行されたトークンのazp(authorized party)クレームと比較する期待値
    externalApiClientId: envDefault('EXTERNAL_API_CLIENT_ID', 'external-api-client'),
    // `backend.external-tasks-pagination-v2`のポーリング間隔(秒)
    // backend(Go)のFeatureFlagPollInterval既定値(10秒)と合わせる
    featureFlagPollIntervalSecs:
      parseInt(envDefault('FEATURE_FLAG_POLL_INTERVAL_SECONDS', '10'), 10) || 10,

    dbDsn: envDefault('DB_DSN', 'mysql://root@127.0.0.1:13306/bff_gin_development'),

    keycloakIssuer,
    keycloakJwksUrl,
    expectedAudience: envDefault('EXPECTED_AUDIENCE', 'backend'),

    localHmacSecret: envDefault('LOCAL_AUTH_HMAC_SECRET', 'local-dev-hmac-shared-secret-change-me'),
    localRsaJwksUrl: envDefault('LOCAL_AUTH_RSA_JWKS_URL', 'http://localhost:8080/.well-known/jwks.json'),

    logLevel: envDefault('LOG_LEVEL', 'info'),
  };
}

module.exports = { loadConfig };
