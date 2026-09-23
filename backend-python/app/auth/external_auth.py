from __future__ import annotations

from app.auth.claims import Claims, VerifyException
from app.auth.dispatcher import Dispatcher, is_local_issuer
from app.domain.errors import TaskError


async def require_external_client(
    dispatcher: Dispatcher, authorization_header: str | None, external_api_client_id: str
) -> Claims:
    """外部公開API専用の認証(CONTRACT.mdセクション11「認証: Client Credentials Grant」)。
    内部REST/gRPCのuser_resolver.resolve()とは別の関数にしている: ローカルHMAC/RSA発行のJWTは
    署名検証自体が正しく通っても、ここでは受け付けない(Keycloak発行のClient Credentials Grant
    トークンのみ許可、azp(authorized party)がexternal_api_client_idと一致することを要求する)。
    他言語(backend-c/backend-cpp/backend-rust/backend-java/backend-kotlin/backend-elixir/
    backend-haskell)のRequireExternalClientAuthと同じ設計
    """
    if authorization_header is None or not authorization_header.startswith("Bearer "):
        raise TaskError.unauthorized()
    token = authorization_header[len("Bearer ") :]

    try:
        claims = await dispatcher.verify(token)
    except VerifyException as exc:
        raise TaskError.unauthorized() from exc

    if is_local_issuer(claims.iss):
        raise TaskError.unauthorized()
    if claims.azp != external_api_client_id:
        raise TaskError.unauthorized()
    return claims
