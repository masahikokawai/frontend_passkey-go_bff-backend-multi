"""backend-java/backend-kotlin/backend-rustのenv var名・既定値と揃えている
(同じdocker-compose環境の上で比較実行できるようにするため)
"""
from __future__ import annotations

import os
from dataclasses import dataclass


def _env_or(key: str, fallback: str) -> str:
    value = os.environ.get(key)
    return value if value else fallback


@dataclass(frozen=True)
class Config:
    http_addr: str
    grpc_addr: str
    external_http_addr: str
    external_api_client_id: str
    db_host: str
    db_port: int
    db_user: str
    db_password: str
    db_schema: str
    keycloak_issuer: str
    keycloak_jwks_url: str
    expected_audience: str
    local_hmac_secret: str
    local_rsa_jwks_url: str
    log_level: str

    @staticmethod
    def from_env() -> "Config":
        keycloak_issuer = _env_or("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training")
        keycloak_jwks_url = os.environ.get("KEYCLOAK_JWKS_URL") or f"{keycloak_issuer}/protocol/openid-connect/certs"
        return Config(
            http_addr=_env_or("HTTP_ADDR", "8115"),
            grpc_addr=_env_or("GRPC_ADDR", "9103"),
            external_http_addr=_env_or("EXTERNAL_HTTP_ADDR", "8116"),
            external_api_client_id=_env_or("EXTERNAL_API_CLIENT_ID", "external-api-client"),
            db_host=_env_or("DB_HOST", "127.0.0.1"),
            db_port=int(_env_or("DB_PORT", "13306")),
            db_user=_env_or("DB_USER", "root"),
            db_password=_env_or("DB_PASSWORD", ""),
            db_schema=_env_or("DB_SCHEMA", "bff_gin_development"),
            keycloak_issuer=keycloak_issuer,
            keycloak_jwks_url=keycloak_jwks_url,
            expected_audience=_env_or("EXPECTED_AUDIENCE", "backend"),
            local_hmac_secret=_env_or("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me"),
            local_rsa_jwks_url=_env_or("LOCAL_AUTH_RSA_JWKS_URL", "http://localhost:8080/.well-known/jwks.json"),
            # backend(Go)/bff/gateway(Go)と同じ既定値・値の集合("debug"/"info"/"warn"/"error")
            log_level=_env_or("LOG_LEVEL", "info"),
        )
