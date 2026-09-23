from __future__ import annotations

from dataclasses import dataclass

from app.domain.errors import TaskError, TaskErrorKind


@dataclass
class StatusAndBody:
    status: int
    body: dict


_KIND_TO_STATUS_AND_ERROR: dict[TaskErrorKind, tuple[int, str]] = {
    TaskErrorKind.UNAUTHORIZED: (401, "unauthorized"),
    TaskErrorKind.USER_NOT_PROVISIONED: (403, "user_not_provisioned"),
    TaskErrorKind.INVALID_REQUEST: (400, "invalid_request"),
    TaskErrorKind.INVALID_ID: (400, "invalid_id"),
    TaskErrorKind.INVALID_STATUS: (422, "invalid_status"),
    TaskErrorKind.INVALID_FINISHED_ON: (422, "invalid_finished_on"),
    TaskErrorKind.NOT_FOUND: (404, "not_found"),
    TaskErrorKind.INTERNAL: (500, "internal_server_error"),
}


def status_and_body(error: TaskError) -> StatusAndBody:
    """backend-java/backend-kotlin/backend-rustのRestErrorMapperと1文字も変えていないJSON形状・
    HTTPステータス(CONTRACT.mdセクション20.5のワイヤー契約パリティ)。
    実サーバーを起動せずに単体テストできるようにするため、statusとbodyの決定だけを切り出している
    (backend-java/backend-kotlinのRestErrorMapperと同じ動機)
    """
    if error.kind == TaskErrorKind.VALIDATION:
        return StatusAndBody(422, {"error": "validation_error", "message": error.message})

    status, error_key = _KIND_TO_STATUS_AND_ERROR[error.kind]
    return StatusAndBody(status, {"error": error_key})
