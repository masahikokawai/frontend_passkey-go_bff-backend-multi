from __future__ import annotations

from dataclasses import dataclass, field
from datetime import date, datetime
from enum import Enum


class TaskStatus(Enum):
    """backend(Go)のTaskStatus(waiting=1, work_in_progress=2, completed=3)と同じマッピング"""

    WAITING = (1, "waiting")
    WORK_IN_PROGRESS = (2, "work_in_progress")
    COMPLETED = (3, "completed")

    def __init__(self, db_value: int, wire_value: str) -> None:
        self.db_value = db_value
        self.wire_value = wire_value

    @staticmethod
    def from_wire_value(value: str) -> "TaskStatus | None":
        for status in TaskStatus:
            if status.wire_value == value:
                return status
        return None

    @staticmethod
    def from_db_value(value: int) -> "TaskStatus | None":
        for status in TaskStatus:
            if status.db_value == value:
                return status
        return None


@dataclass
class Label:
    id: int
    name: str


@dataclass
class User:
    id: int
    email: str
    name: str


@dataclass
class TaskInput:
    name: str
    description: str | None
    status_raw: str
    finished_on: date | None
    label_ids: list[int] = field(default_factory=list)


@dataclass
class Task:
    """user_idはワイヤーに乗せない(REST/gRPCとも)。backend(Go)実装がtaskDTOToJSONで
    user_idを含めていない実際の挙動に合わせている(CONTRACT.mdセクション5.1本文の例には
    書かれているが、ワイヤー契約パリティの原則(セクション20.5)により、ドキュメントではなく
    実際の挙動に合わせる。backend-java/backend-kotlin/backend-rust/backend-c/backend-cppの
    全てで同じ既知の差異が確認・踏襲されている)
    """

    id: int
    name: str
    description: str | None
    status: TaskStatus
    finished_on: date
    user_id: int
    labels: list[Label]
    created_at: datetime
    updated_at: datetime
