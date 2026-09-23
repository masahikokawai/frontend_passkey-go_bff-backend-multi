from __future__ import annotations

import pytest

from app.auth.dispatcher import LOCAL_HMAC_ISSUER, Dispatcher
from app.auth.external_auth import require_external_client
from app.auth.hmac_verifier import HmacVerifier
from app.auth.jwks_verifier import JwksVerifier
from app.domain.errors import TaskError
from tests.unit.support.mock_jwks_server import MockJwksServer
from tests.unit.support.test_token_helper import generate_rsa_key_pair, make_hmac_token, make_rsa_token

HMAC_SECRET = "test-secret-at-least-32-bytes-long!!"
KEYCLOAK_ISSUER = "http://localhost:8082/realms/training"
EXTERNAL_CLIENT_ID = "external-api-client"


async def test_accepts_keycloak_token_with_matching_azp() -> None:
    with MockJwksServer() as jwks:
        private_key = generate_rsa_key_pair()
        jwks.add_key("kid-1", private_key.public_key())
        dispatcher = Dispatcher().register(
            KEYCLOAK_ISSUER, JwksVerifier(jwks.jwks_url(), KEYCLOAK_ISSUER, "backend")
        )
        token = make_rsa_token(
            private_key, "kid-1", KEYCLOAK_ISSUER, "backend", "user-sub", 3600, azp=EXTERNAL_CLIENT_ID
        )
        claims = await require_external_client(dispatcher, f"Bearer {token}", EXTERNAL_CLIENT_ID)
        assert claims.azp == EXTERNAL_CLIENT_ID


async def test_rejects_keycloak_token_with_wrong_azp() -> None:
    with MockJwksServer() as jwks:
        private_key = generate_rsa_key_pair()
        jwks.add_key("kid-1", private_key.public_key())
        dispatcher = Dispatcher().register(
            KEYCLOAK_ISSUER, JwksVerifier(jwks.jwks_url(), KEYCLOAK_ISSUER, "backend")
        )
        token = make_rsa_token(
            private_key, "kid-1", KEYCLOAK_ISSUER, "backend", "user-sub", 3600, azp="some-other-client"
        )
        with pytest.raises(TaskError):
            await require_external_client(dispatcher, f"Bearer {token}", EXTERNAL_CLIENT_ID)


async def test_rejects_keycloak_token_with_missing_azp() -> None:
    with MockJwksServer() as jwks:
        private_key = generate_rsa_key_pair()
        jwks.add_key("kid-1", private_key.public_key())
        dispatcher = Dispatcher().register(
            KEYCLOAK_ISSUER, JwksVerifier(jwks.jwks_url(), KEYCLOAK_ISSUER, "backend")
        )
        token = make_rsa_token(private_key, "kid-1", KEYCLOAK_ISSUER, "backend", "user-sub", 3600)
        with pytest.raises(TaskError):
            await require_external_client(dispatcher, f"Bearer {token}", EXTERNAL_CLIENT_ID)


async def test_rejects_local_hmac_issuer_even_with_correct_azp() -> None:
    """ローカルHMAC発行のJWTは、署名検証自体が正しく通り、かつazpが一致していても、
    外部公開APIでは受け付けない(Keycloak発行のClient Credentials Grantトークンのみ許可)。
    これはこの認証チェックの最も重要な、逆に間違えやすい振る舞いである
    """
    dispatcher = Dispatcher().register(
        LOCAL_HMAC_ISSUER, HmacVerifier(HMAC_SECRET, LOCAL_HMAC_ISSUER, "backend")
    )
    token = make_hmac_token(HMAC_SECRET, LOCAL_HMAC_ISSUER, "backend", "1", 3600, azp=EXTERNAL_CLIENT_ID)
    with pytest.raises(TaskError):
        await require_external_client(dispatcher, f"Bearer {token}", EXTERNAL_CLIENT_ID)


async def test_rejects_missing_authorization_header() -> None:
    dispatcher = Dispatcher()
    with pytest.raises(TaskError):
        await require_external_client(dispatcher, None, EXTERNAL_CLIENT_ID)


async def test_rejects_malformed_bearer_header() -> None:
    dispatcher = Dispatcher()
    with pytest.raises(TaskError):
        await require_external_client(dispatcher, "not-a-bearer-header", EXTERNAL_CLIENT_ID)
