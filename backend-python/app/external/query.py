from __future__ import annotations

from dataclasses import dataclass


class ExternalQueryError(Exception):
    """外部公開API専用のクエリパラメータエラー。内部API用のTaskError/TaskErrorKindとは
    別の、この2エンドポイント専用の400系エラーコード(user_id_required/invalid_user_id)を
    持つため、内部のerror_mapperには寄せず専用の例外にしている(CONTRACT.mdセクション11)
    """

    def __init__(self, error_key: str) -> None:
        super().__init__(error_key)
        self.error_key = error_key


@dataclass
class OffsetQuery:
    page: int
    page_size: int


@dataclass
class CursorQuery:
    after_id: int
    limit: int


def parse_user_id(raw: str | None) -> int:
    """CONTRACT.mdセクション11: user_idは必須。クライアント側が任意に指定できる
    (サーバー間の信頼関係を前提にした設計であり、エンドユーザー単位の認可は行わない、
    既知の制約としてREADME.mdに明記する)
    """
    if raw is None or raw == "":
        raise ExternalQueryError("user_id_required")
    try:
        return int(raw)
    except ValueError as exc:
        raise ExternalQueryError("invalid_user_id") from exc


def parse_offset_query(raw_page: str | None, raw_page_size: str | None) -> OffsetQuery:
    page = 1
    if raw_page:
        try:
            page = max(1, int(raw_page))
        except ValueError:
            pass
    page_size = 10
    if raw_page_size:
        try:
            page_size = max(1, int(raw_page_size))
        except ValueError:
            pass
    return OffsetQuery(page=page, page_size=page_size)


def parse_cursor_query(raw_cursor: str | None, raw_limit: str | None) -> CursorQuery:
    after_id = 0
    if raw_cursor:
        try:
            after_id = max(0, int(raw_cursor))
        except ValueError:
            pass
    limit = 10
    if raw_limit:
        try:
            limit = max(1, int(raw_limit))
        except ValueError:
            pass
    return CursorQuery(after_id=after_id, limit=limit)
