from __future__ import annotations

import pytest

from app.external.query import ExternalQueryError, parse_cursor_query, parse_offset_query, parse_user_id


def test_parse_user_id_accepts_valid_numeric_string() -> None:
    assert parse_user_id("42") == 42


def test_parse_user_id_rejects_missing() -> None:
    with pytest.raises(ExternalQueryError) as exc_info:
        parse_user_id(None)
    assert exc_info.value.error_key == "user_id_required"


def test_parse_user_id_rejects_empty_string() -> None:
    with pytest.raises(ExternalQueryError) as exc_info:
        parse_user_id("")
    assert exc_info.value.error_key == "user_id_required"


def test_parse_user_id_rejects_non_numeric() -> None:
    with pytest.raises(ExternalQueryError) as exc_info:
        parse_user_id("abc")
    assert exc_info.value.error_key == "invalid_user_id"


def test_parse_offset_query_defaults() -> None:
    q = parse_offset_query(None, None)
    assert q.page == 1
    assert q.page_size == 10


def test_parse_offset_query_uses_given_values() -> None:
    q = parse_offset_query("3", "25")
    assert q.page == 3
    assert q.page_size == 25


def test_parse_offset_query_clamps_below_one() -> None:
    q = parse_offset_query("0", "-5")
    assert q.page == 1
    assert q.page_size == 1


def test_parse_offset_query_ignores_non_numeric() -> None:
    q = parse_offset_query("abc", "xyz")
    assert q.page == 1
    assert q.page_size == 10


def test_parse_cursor_query_defaults_to_start() -> None:
    q = parse_cursor_query(None, None)
    assert q.after_id == 0
    assert q.limit == 10


def test_parse_cursor_query_uses_given_values() -> None:
    q = parse_cursor_query("42", "5")
    assert q.after_id == 42
    assert q.limit == 5


def test_parse_cursor_query_clamps_negative_cursor_to_zero() -> None:
    q = parse_cursor_query("-1", "5")
    assert q.after_id == 0


def test_parse_cursor_query_clamps_limit_below_one() -> None:
    q = parse_cursor_query("0", "0")
    assert q.limit == 1
