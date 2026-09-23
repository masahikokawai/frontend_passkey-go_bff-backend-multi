from __future__ import annotations

from typing import Protocol

from app.auth.claims import Claims


class TokenVerifier(Protocol):
    """JwksVerifier実装がkid不一致時にJWKSをネットワーク越しに再取得する(httpxのasync呼び出し)
    必要があるため、非同期メソッドにしている。HmacVerifierは実際には何もawaitしない
    (PyJWTの同期検証のみ)が、インターフェースは統一している
    """

    async def verify(self, token: str) -> Claims: ...
