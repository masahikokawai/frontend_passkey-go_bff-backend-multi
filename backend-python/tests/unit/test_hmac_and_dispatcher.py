from __future__ import annotations

import pytest

from app.auth.claims import VerifyException
from app.auth.dispatcher import LOCAL_HMAC_ISSUER, LOCAL_RSA_ISSUER, Dispatcher, is_local_issuer
from app.auth.hmac_verifier import HmacVerifier
from tests.unit.support.test_token_helper import make_alg_none_token, make_hmac_token

SECRET = "test-secret-at-least-32-bytes-long!!"


def test_is_local_issuer_matches_hmac_and_rsa_only() -> None:
    assert is_local_issuer(LOCAL_HMAC_ISSUER)
    assert is_local_issuer(LOCAL_RSA_ISSUER)
    assert not is_local_issuer("http://localhost:8082/realms/training")
    assert not is_local_issuer("")


async def test_hmac_verifier_accepts_valid_token() -> None:
    token = make_hmac_token(SECRET, LOCAL_HMAC_ISSUER, "backend", "42", 3600)
    verifier = HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, "backend")
    claims = await verifier.verify(token)
    assert claims.sub == "42"
    assert claims.iss == LOCAL_HMAC_ISSUER


async def test_hmac_verifier_rejects_wrong_secret() -> None:
    token = make_hmac_token(SECRET, LOCAL_HMAC_ISSUER, "backend", "42", 3600)
    verifier = HmacVerifier("different-secret-also-32-bytes-long!", LOCAL_HMAC_ISSUER, "backend")
    with pytest.raises(VerifyException):
        await verifier.verify(token)


async def test_hmac_verifier_rejects_expired_token() -> None:
    token = make_hmac_token(SECRET, LOCAL_HMAC_ISSUER, "backend", "42", -3600)
    verifier = HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, "backend")
    with pytest.raises(VerifyException):
        await verifier.verify(token)


async def test_hmac_verifier_rejects_wrong_audience() -> None:
    token = make_hmac_token(SECRET, LOCAL_HMAC_ISSUER, "someone-else", "42", 3600)
    verifier = HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, "backend")
    with pytest.raises(VerifyException):
        await verifier.verify(token)


async def test_hmac_verifier_rejects_wrong_issuer() -> None:
    token = make_hmac_token(SECRET, "some-other-issuer", "backend", "42", 3600)
    verifier = HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, "backend")
    with pytest.raises(VerifyException):
        await verifier.verify(token)


async def test_hmac_verifier_rejects_unexpected_algorithm() -> None:
    """セキュリティ上重要な確認: ヘッダのalgが期待(HS256)と異なる場合は、
    仮に鍵が正しくても拒否されること(アルゴリズム混同攻撃対策)
    """
    token = make_hmac_token(SECRET, LOCAL_HMAC_ISSUER, "backend", "42", 3600, alg="HS384")
    verifier = HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, "backend")
    with pytest.raises(VerifyException):
        await verifier.verify(token)


async def test_hmac_verifier_rejects_alg_none() -> None:
    token = make_alg_none_token(LOCAL_HMAC_ISSUER, "backend", "42", 3600)
    verifier = HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, "backend")
    with pytest.raises(VerifyException):
        await verifier.verify(token)


async def test_dispatcher_routes_by_issuer_and_rejects_unknown_issuer() -> None:
    dispatcher = Dispatcher().register(LOCAL_HMAC_ISSUER, HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, "backend"))

    good_token = make_hmac_token(SECRET, LOCAL_HMAC_ISSUER, "backend", "7", 3600)
    claims = await dispatcher.verify(good_token)
    assert claims.sub == "7"

    unknown_issuer_token = make_hmac_token(SECRET, "unknown-issuer", "backend", "7", 3600)
    with pytest.raises(VerifyException):
        await dispatcher.verify(unknown_issuer_token)


async def test_dispatcher_rejects_malformed_token() -> None:
    dispatcher = Dispatcher().register(LOCAL_HMAC_ISSUER, HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, "backend"))
    with pytest.raises(VerifyException):
        await dispatcher.verify("not-a-jwt")
