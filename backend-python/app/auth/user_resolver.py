from __future__ import annotations

import logging

from app.auth.claims import VerifyException
from app.auth.dispatcher import Dispatcher, is_local_issuer
from app.domain.errors import TaskError
from app.repository.task_repository import TaskRepository

log = logging.getLogger(__name__)


class UserResolver:
    """REST/gRPCの両トランスポートが共有するuser_id解決ロジック(認証ロジックを複製しない設計)。
    backend(Go)のresolveUserID・backend-rust/backend-java/backend-kotlinのUserResolverと同じ分岐:
        - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの -> usersをidで検索
        - Keycloak発行のJWT: subはkeycloak_sub -> user_keycloaks経由で検索
    どちらも見つからなければUserNotProvisioned
    """

    def __init__(self, dispatcher: Dispatcher, repository: TaskRepository) -> None:
        self._dispatcher = dispatcher
        self._repository = repository

    async def resolve(self, authorization_header: str | None) -> int:
        if authorization_header is None or not authorization_header.startswith("Bearer "):
            raise TaskError.unauthorized()
        token = authorization_header[len("Bearer ") :]

        try:
            claims = await self._dispatcher.verify(token)
        except VerifyException as exc:
            raise TaskError.unauthorized() from exc

        if is_local_issuer(claims.iss):
            try:
                user_id = int(claims.sub)
            except ValueError as exc:
                raise TaskError.user_not_provisioned() from exc
            user = await self._repository.find_user_by_id(user_id)
            if user is None:
                raise TaskError.user_not_provisioned()
            log.debug("resolved user_id=%d via local issuer=%s", user.id, claims.iss)
            return user.id

        user = await self._repository.find_user_by_keycloak_sub(claims.sub)
        if user is None:
            raise TaskError.user_not_provisioned()
        log.debug("resolved user_id=%d via keycloak_sub=%s", user.id, claims.sub)
        return user.id
