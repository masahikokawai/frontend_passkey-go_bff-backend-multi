from __future__ import annotations

import base64
import json
import time

import jwt as pyjwt
from cryptography.hazmat.primitives.asymmetric import rsa


def make_hmac_token(
    secret: str, iss: str, aud: str, sub: str, exp_offset_secs: int, alg: str = "HS256", azp: str | None = None
) -> str:
    payload = {"sub": sub, "iss": iss, "aud": aud, "exp": int(time.time()) + exp_offset_secs}
    if azp is not None:
        payload["azp"] = azp
    return pyjwt.encode(payload, secret, algorithm=alg)


def make_alg_none_token(iss: str, aud: str, sub: str, exp_offset_secs: int) -> str:
    """"alg":"none"は署名の要らない自己主張トークンになるため、必ず拒否されなければならない。
    PyJWTのencode()はセキュリティ上デフォルトでalg=noneを許可しないため、手動で組み立てる
    (backend-java/backend-kotlinの同名テストと同じ意図)
    """
    header = {"alg": "none", "typ": "JWT"}
    payload = {"sub": sub, "iss": iss, "aud": aud, "exp": int(time.time()) + exp_offset_secs}
    header_b64 = _b64url(json.dumps(header).encode())
    payload_b64 = _b64url(json.dumps(payload).encode())
    return f"{header_b64}.{payload_b64}."


def generate_rsa_key_pair() -> rsa.RSAPrivateKey:
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


def make_rsa_token(
    private_key: rsa.RSAPrivateKey,
    kid: str,
    iss: str,
    aud: str,
    sub: str,
    exp_offset_secs: int,
    alg: str = "RS256",
    azp: str | None = None,
) -> str:
    payload = {"sub": sub, "iss": iss, "aud": aud, "exp": int(time.time()) + exp_offset_secs}
    if azp is not None:
        payload["azp"] = azp
    return pyjwt.encode(payload, private_key, algorithm=alg, headers={"kid": kid})


def rsa_modulus_b64url(public_numbers) -> str:
    return _b64url_uint(public_numbers.n)


def rsa_exponent_b64url(public_numbers) -> str:
    return _b64url_uint(public_numbers.e)


def _b64url_uint(value: int) -> str:
    length = (value.bit_length() + 7) // 8
    return _b64url(value.to_bytes(length, "big"))


def _b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()
