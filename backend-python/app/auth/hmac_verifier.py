from __future__ import annotations

import jwt as pyjwt

from app.auth.claims import Claims, VerifyException


class HmacVerifier:
    """ローカルHMAC発行(iss=dispatcher.LOCAL_HMAC_ISSUER)の検証。HS256、共有シークレット"""

    def __init__(self, secret: str, issuer: str, audience: str) -> None:
        self._secret = secret
        self._issuer = issuer
        self._audience = audience

    async def verify(self, token: str) -> Claims:
        try:
            header = pyjwt.get_unverified_header(token)
        except pyjwt.PyJWTError as exc:
            raise VerifyException(f"failed to parse JWT header: {exc}") from exc

        # 【セキュリティ上の確認、backend-java/backend-kotlinと同じ観点】
        # PyJWTのjwt.decode(algorithms=[...])は、指定したアルゴリズム以外(RS256や"none"含む)の
        # トークンをそもそも受理しないが、ヘッダのalgが期待通りHS256であることも明示的に
        # 確認する(多層防御、アルゴリズム混同攻撃対策)
        if header.get("alg") != "HS256":
            raise VerifyException(f"unexpected alg: {header.get('alg')}")

        try:
            claims = pyjwt.decode(
                token,
                self._secret,
                algorithms=["HS256"],
                issuer=self._issuer,
                audience=self._audience,
            )
        except pyjwt.PyJWTError as exc:
            raise VerifyException(f"HMAC verify failed: {exc}") from exc

        return Claims(sub=str(claims["sub"]), iss=claims["iss"], azp=claims.get("azp", ""))
