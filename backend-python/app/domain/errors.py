from __future__ import annotations

from enum import Enum, auto


class TaskErrorKind(Enum):
    UNAUTHORIZED = auto()
    USER_NOT_PROVISIONED = auto()
    INVALID_REQUEST = auto()
    INVALID_ID = auto()
    INVALID_STATUS = auto()
    INVALID_FINISHED_ON = auto()
    VALIDATION = auto()
    NOT_FOUND = auto()
    INTERNAL = auto()


class TaskError(Exception):
    """backend-java/backend-kotlin/backend-rustのTaskError/RestErrorと同じ分類。
    JSON形状・HTTPステータス・gRPCステータスへの変換はトランスポート層(rest/grpc_)がそれぞれ担う
    """

    def __init__(self, kind: TaskErrorKind, message: str) -> None:
        super().__init__(message)
        self.kind = kind
        self.message = message

    @staticmethod
    def unauthorized() -> "TaskError":
        return TaskError(TaskErrorKind.UNAUTHORIZED, "unauthorized")

    @staticmethod
    def user_not_provisioned() -> "TaskError":
        return TaskError(TaskErrorKind.USER_NOT_PROVISIONED, "user_not_provisioned")

    @staticmethod
    def invalid_request() -> "TaskError":
        return TaskError(TaskErrorKind.INVALID_REQUEST, "invalid_request")

    @staticmethod
    def invalid_id() -> "TaskError":
        return TaskError(TaskErrorKind.INVALID_ID, "invalid_id")

    @staticmethod
    def invalid_status() -> "TaskError":
        return TaskError(TaskErrorKind.INVALID_STATUS, "invalid_status")

    @staticmethod
    def invalid_finished_on() -> "TaskError":
        return TaskError(TaskErrorKind.INVALID_FINISHED_ON, "invalid_finished_on")

    @staticmethod
    def validation(message: str) -> "TaskError":
        return TaskError(TaskErrorKind.VALIDATION, message)

    @staticmethod
    def not_found() -> "TaskError":
        return TaskError(TaskErrorKind.NOT_FOUND, "not_found")

    @staticmethod
    def internal(message: str) -> "TaskError":
        return TaskError(TaskErrorKind.INTERNAL, message)
