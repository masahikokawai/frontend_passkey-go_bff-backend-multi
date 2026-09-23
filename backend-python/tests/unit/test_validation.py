from __future__ import annotations

from datetime import date, timedelta

import pytest

from app.domain.errors import TaskError, TaskErrorKind
from app.domain.models import TaskInput, TaskStatus
from app.domain.validation import validate_task_input


def _valid_input(today: date) -> TaskInput:
    return TaskInput(name="buy milk", description=None, status_raw="waiting", finished_on=today, label_ids=[])


def test_accepts_valid_input() -> None:
    today = date(2026, 9, 9)
    status = validate_task_input(_valid_input(today), today)
    assert status == TaskStatus.WAITING


def test_rejects_empty_name() -> None:
    today = date(2026, 9, 9)
    input_ = TaskInput(name="", description=None, status_raw="waiting", finished_on=today, label_ids=[])
    with pytest.raises(TaskError) as exc_info:
        validate_task_input(input_, today)
    assert exc_info.value.kind == TaskErrorKind.VALIDATION


def test_rejects_name_over_20_codepoints() -> None:
    today = date(2026, 9, 9)
    input_ = TaskInput(name="a" * 21, description=None, status_raw="waiting", finished_on=today, label_ids=[])
    with pytest.raises(TaskError) as exc_info:
        validate_task_input(input_, today)
    assert exc_info.value.kind == TaskErrorKind.VALIDATION


def test_accepts_name_exactly_20_codepoints() -> None:
    today = date(2026, 9, 9)
    input_ = TaskInput(name="a" * 20, description=None, status_raw="waiting", finished_on=today, label_ids=[])
    assert validate_task_input(input_, today) == TaskStatus.WAITING


def test_accepts_astral_emoji_name_of_20_codepoints() -> None:
    """他言語で見つかった既知の落とし穴: 絵文字(基本多言語面外、サロゲートペア)を含む名前で
    コードポイント数とUTF-16コード単位数がずれる境界値。Python 3のstrは常にコードポイント単位で
    格納されるため(PEP 393)、この落とし穴自体が存在しない(app/domain/validation.pyの
    docstring参照)
    """
    today = date(2026, 9, 9)
    emoji_name = "😀" * 20
    input_ = TaskInput(name=emoji_name, description=None, status_raw="waiting", finished_on=today, label_ids=[])
    assert validate_task_input(input_, today) == TaskStatus.WAITING


def test_rejects_astral_emoji_name_of_21_codepoints() -> None:
    today = date(2026, 9, 9)
    emoji_name = "😀" * 21
    input_ = TaskInput(name=emoji_name, description=None, status_raw="waiting", finished_on=today, label_ids=[])
    with pytest.raises(TaskError) as exc_info:
        validate_task_input(input_, today)
    assert exc_info.value.kind == TaskErrorKind.VALIDATION


def test_rejects_past_finished_on() -> None:
    today = date(2026, 9, 9)
    input_ = TaskInput(
        name="x", description=None, status_raw="waiting", finished_on=today - timedelta(days=1), label_ids=[]
    )
    with pytest.raises(TaskError) as exc_info:
        validate_task_input(input_, today)
    assert exc_info.value.kind == TaskErrorKind.VALIDATION


def test_accepts_finished_on_equal_to_today() -> None:
    today = date(2026, 9, 9)
    input_ = TaskInput(name="x", description=None, status_raw="waiting", finished_on=today, label_ids=[])
    assert validate_task_input(input_, today) == TaskStatus.WAITING


def test_rejects_unknown_status() -> None:
    today = date(2026, 9, 9)
    input_ = TaskInput(name="x", description=None, status_raw="not_a_status", finished_on=today, label_ids=[])
    with pytest.raises(TaskError) as exc_info:
        validate_task_input(input_, today)
    assert exc_info.value.kind == TaskErrorKind.VALIDATION


def test_status_round_trips_through_wire_and_db_value() -> None:
    for status in TaskStatus:
        assert TaskStatus.from_wire_value(status.wire_value) == status
        assert TaskStatus.from_db_value(status.db_value) == status
    assert TaskStatus.from_wire_value("bogus") is None
    assert TaskStatus.from_db_value(0) is None
    assert TaskStatus.from_db_value(99) is None
