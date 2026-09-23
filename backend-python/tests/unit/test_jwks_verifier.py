from __future__ import annotations

import pytest

from app.auth.claims import VerifyException
from app.auth.jwks_verifier import JwksVerifier
from tests.unit.support.mock_jwks_server import MockJwksServer
from tests.unit.support.test_token_helper import generate_rsa_key_pair, make_rsa_token

ISSUER = "https://issuer.example"


async def test_accepts_valid_token_signed_with_known_key() -> None:
    key_pair = generate_rsa_key_pair()
    with MockJwksServer() as mock_jwks:
        mock_jwks.add_key("kid-1", key_pair.public_key())
        verifier = JwksVerifier(mock_jwks.jwks_url(), ISSUER, "backend")

        token = make_rsa_token(key_pair, "kid-1", ISSUER, "backend", "99", 3600)
        claims = await verifier.verify(token)
        assert claims.sub == "99"
        assert claims.iss == ISSUER


async def test_unknown_kid_triggers_refresh_then_succeeds_if_now_present() -> None:
    key_pair = generate_rsa_key_pair()
    with MockJwksServer() as mock_jwks:
        verifier = JwksVerifier(mock_jwks.jwks_url(), ISSUER, "backend")
        # JwksVerifier生成時点ではまだ鍵ゼロ件 -> 最初の検証はkid不一致で1回だけ再取得を試みる
        mock_jwks.add_key("kid-2", key_pair.public_key())

        token = make_rsa_token(key_pair, "kid-2", ISSUER, "backend", "1", 3600)
        claims = await verifier.verify(token)
        assert claims.sub == "1"


async def test_unknown_kid_still_unknown_after_refresh_is_rejected() -> None:
    key_pair = generate_rsa_key_pair()
    with MockJwksServer() as mock_jwks:
        mock_jwks.add_key("kid-registered", key_pair.public_key())
        verifier = JwksVerifier(mock_jwks.jwks_url(), ISSUER, "backend")

        token = make_rsa_token(key_pair, "kid-does-not-exist", ISSUER, "backend", "1", 3600)
        with pytest.raises(VerifyException):
            await verifier.verify(token)


async def test_wrong_issuer_is_rejected_even_with_valid_signature() -> None:
    key_pair = generate_rsa_key_pair()
    with MockJwksServer() as mock_jwks:
        mock_jwks.add_key("kid-1", key_pair.public_key())
        verifier = JwksVerifier(mock_jwks.jwks_url(), ISSUER, "backend")

        token = make_rsa_token(key_pair, "kid-1", "https://different-issuer.example", "backend", "1", 3600)
        with pytest.raises(VerifyException):
            await verifier.verify(token)


async def test_wrong_audience_is_rejected() -> None:
    key_pair = generate_rsa_key_pair()
    with MockJwksServer() as mock_jwks:
        mock_jwks.add_key("kid-1", key_pair.public_key())
        verifier = JwksVerifier(mock_jwks.jwks_url(), ISSUER, "backend")

        token = make_rsa_token(key_pair, "kid-1", ISSUER, "someone-else", "1", 3600)
        with pytest.raises(VerifyException):
            await verifier.verify(token)


async def test_expired_token_is_rejected() -> None:
    key_pair = generate_rsa_key_pair()
    with MockJwksServer() as mock_jwks:
        mock_jwks.add_key("kid-1", key_pair.public_key())
        verifier = JwksVerifier(mock_jwks.jwks_url(), ISSUER, "backend")

        token = make_rsa_token(key_pair, "kid-1", ISSUER, "backend", "1", -3600)
        with pytest.raises(VerifyException):
            await verifier.verify(token)


async def test_rejects_unexpected_algorithm() -> None:
    """アルゴリズム混同攻撃対策: ヘッダのalgがRS256以外なら拒否する"""
    key_pair = generate_rsa_key_pair()
    with MockJwksServer() as mock_jwks:
        mock_jwks.add_key("kid-1", key_pair.public_key())
        verifier = JwksVerifier(mock_jwks.jwks_url(), ISSUER, "backend")

        token = make_rsa_token(key_pair, "kid-1", ISSUER, "backend", "1", 3600, alg="RS512")
        with pytest.raises(VerifyException):
            await verifier.verify(token)
