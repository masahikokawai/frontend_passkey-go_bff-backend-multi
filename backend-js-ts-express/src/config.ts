// backend-js/src/config.jsの型付き移植。ロジックは変更していない。

import type { AppConfig } from './types';

function envDefault(key: string, fallback: string): string {
  const v = process.env[key];
  return v === undefined || v === '' ? fallback : v;
}

export function loadConfig(): AppConfig {
  const keycloakIssuer = envDefault('KEYCLOAK_ISSUER', 'http://localhost:8082/realms/training');
  const keycloakJwksUrl = envDefault(
    'KEYCLOAK_JWKS_URL',
    `${keycloakIssuer}/protocol/openid-connect/certs`,
  );

  return {
    // 他言語(Go:8090、Rust:8093、Scala(http4s):8094、Scala(Pekko):8095、Rails:8096、
    // backend-js:8103)と衝突しない値として:8104を既定にした
    httpAddr: envDefault('HTTP_ADDR', ':8104'),
    grpcAddr: envDefault('GRPC_ADDR', ':9098'),
    // gateway(:8081)・他言語の外部アドレス(Go:8097、Rust:8098、http4s:8099、Pekko:8100、Rails:8101、
    // bff-rails:8102、backend-js:8107)と衝突しない値として:8108を既定にした
    externalHttpAddr: envDefault('EXTERNAL_HTTP_ADDR', ':8108'),
    externalApiClientId: envDefault('EXTERNAL_API_CLIENT_ID', 'external-api-client'),
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
