from __future__ import annotations

from dataclasses import dataclass


@dataclass
class Claims:
    """user_id解決(UserResolver)にはsub/issのみ必要だが、外部公開API(RequireExternalClientAuth)は
    azp(authorized party)クレームでClient Credentials Grantのクライアントを識別する必要があるため保持する
    """

    sub: str
    iss: str
    azp: str = ""


class VerifyException(Exception):
    pass
