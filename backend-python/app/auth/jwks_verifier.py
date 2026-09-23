from __future__ import annotations

import asyncio
import json
import logging

import httpx
import jwt as pyjwt
from jwt.algorithms import RSAAlgorithm

from app.auth.claims import Claims, VerifyException

log = logging.getLogger(__name__)


class JwksVerifier:
    """Keycloak発行・ローカルRSA発行(iss=dispatcher.LOCAL_RSA_ISSUER)共通のJWKSベース検証。RS256。
    kid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけJWKSを再取得する
    (backend(Go)のjwks.go・backend-rust/backend-c/backend-cpp/backend-java/backend-kotlinと
    同じ「kid不一致時のみ再取得」戦略)。

    【コルーチンネイティブなHTTP呼び出し】JWKS取得はhttpxの非同期クライアント(async with
    httpx.AsyncClient() as client: await client.get(...))を使う。同期の`requests`ライブラリを
    async関数の中で呼ぶと、その1箇所だけイベントループのスレッドを実際にブロックしてしまい、
    「ルーティングからDBアクセスまでコルーチンネイティブに貫く」という設計が崩れる
    (backend-kotlinがKtor Clientを使ったのと同じ理由、README.md「アーキテクチャ選定」節参照)。
    再取得の排他制御には`asyncio.Lock`を使う(`threading.Lock`はイベントループをブロックする
    ため誤り、backend-kotlinの`Mutex`/`withLock`と同じ設計判断)
    """

    def __init__(self, jwks_url: str, issuer: str, audience: str, timeout: float = 5.0) -> None:
        self._jwks_url = jwks_url
        self._issuer = issuer
        self._audience = audience
        self._timeout = timeout
        self._keys: dict[str, RSAAlgorithm] = {}
        self._refresh_lock = asyncio.Lock()

    async def verify(self, token: str) -> Claims:
        try:
            header = pyjwt.get_unverified_header(token)
        except pyjwt.PyJWTError as exc:
            raise VerifyException(f"failed to parse JWT header: {exc}") from exc

        kid = header.get("kid")
        if not kid:
            raise VerifyException("JWT header has no kid")

        key = self._keys.get(kid)
        if key is None:
            log.debug("kid=%s not cached for issuer=%s, refreshing JWKS from %s", kid, self._issuer, self._jwks_url)
            await self._refresh()
            key = self._keys.get(kid)
            if key is None:
                raise VerifyException(f"no public key found for kid={kid}")

        # 【セキュリティ上の確認、backend-java/backend-kotlinと同じ観点】アルゴリズム混同攻撃対策
        if header.get("alg") != "RS256":
            raise VerifyException(f"unexpected alg: {header.get('alg')}")

        try:
            claims = pyjwt.decode(
                token,
                key=key,
                algorithms=["RS256"],
                issuer=self._issuer,
                audience=self._audience,
            )
        except pyjwt.PyJWTError as exc:
            raise VerifyException(f"RSA/JWKS verify failed: {exc}") from exc

        return Claims(sub=str(claims["sub"]), iss=claims["iss"], azp=claims.get("azp", ""))

    async def _refresh(self) -> None:
        async with self._refresh_lock:
            try:
                async with httpx.AsyncClient(timeout=self._timeout) as client:
                    response = await client.get(self._jwks_url)
                if response.status_code != 200:
                    raise VerifyException(f"JWKS fetch failed: HTTP {response.status_code}")
                body = response.json()
            except VerifyException:
                raise
            except Exception as exc:
                raise VerifyException(f"JWKS fetch failed: {exc}") from exc

            new_keys: dict[str, RSAAlgorithm] = {}
            for jwk in body.get("keys", []):
                kty = jwk.get("kty", "")
                use = jwk.get("use", "")
                if kty != "RSA" or (use and use != "sig"):
                    continue
                kid = jwk.get("kid", "")
                n = jwk.get("n", "")
                e = jwk.get("e", "")
                if not kid or not n or not e:
                    continue
                try:
                    new_keys[kid] = RSAAlgorithm.from_jwk(json.dumps(jwk))
                except Exception:
                    # 【バッド/グッドプラクティス、backend-java/backend-kotlinと同じ設計】
                    # 特定の鍵1件の構築に失敗しただけでJWKS取得全体を失敗させると、
                    # 他の正常な鍵まで使えなくなってしまう(1つの壊れたエントリが全体を巻き込む)。
                    # ここでは該当エントリだけスキップし、他の鍵は正常にキャッシュへ反映する
                    continue
            self._keys = new_keys
            log.debug("JWKS refreshed for issuer=%s: %d key(s) cached", self._issuer, len(new_keys))
