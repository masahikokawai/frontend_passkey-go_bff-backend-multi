from __future__ import annotations

import base64
import json

from app.auth.claims import Claims, VerifyException
from app.auth.token_verifier import TokenVerifier

LOCAL_HMAC_ISSUER = "bff-gin-local-hmac"
LOCAL_RSA_ISSUER = "bff-gin-local-rsa"


def is_local_issuer(iss: str) -> bool:
    return iss in (LOCAL_HMAC_ISSUER, LOCAL_RSA_ISSUER)


def _pad_base64url(s: str) -> str:
    rem = len(s) % 4
    return s + "=" * (4 - rem) if rem else s


class Dispatcher:
    """JWTの`iss`クレームで検証方式(Keycloak/ローカルHMAC/ローカルRSA)を振り分ける
    (backend(Go)のauthjwt.Dispatcher・backend-rust/backend-c/backend-cpp/backend-java/
    backend-kotlinと同じ2段構造)。issの詐称は委譲先の署名検証で弾かれる:
    「振り分けのために覗く」ことと「検証を信頼する」ことは別、という2段階構造を維持する
    """

    def __init__(self) -> None:
        self._by_issuer: dict[str, TokenVerifier] = {}

    def register(self, issuer: str, verifier: TokenVerifier) -> "Dispatcher":
        self._by_issuer[issuer] = verifier
        return self

    async def verify(self, token: str) -> Claims:
        parts = token.split(".")
        if len(parts) != 3:
            raise VerifyException("malformed JWT")

        try:
            payload_bytes = base64.urlsafe_b64decode(_pad_base64url(parts[1]))
            payload = json.loads(payload_bytes)
            iss = payload.get("iss")
            if iss is None:
                raise VerifyException("unknown issuer: null")
        except VerifyException:
            raise
        except Exception as exc:
            raise VerifyException("failed to peek iss claim") from exc

        verifier = self._by_issuer.get(iss)
        if verifier is None:
            raise VerifyException(f"unknown issuer: {iss}")
        return await verifier.verify(token)
