from __future__ import annotations

from app.domain.models import Task

RFC3339_FORMAT = "%Y-%m-%dT%H:%M:%S+00:00"


def task_to_json(task: Task) -> dict:
    """CONTRACT.mdセクション5.1のJSON形状(スネークケース)。
    【backend(Go)の実際の挙動に合わせた既知の差異】taskDTOToJSON(backend/internal/handler/v1/task.go)は
    user_idをレスポンスに含めていない(CONTRACT.md本文の例には書かれているが、実装はそうなっていない。
    ワイヤー契約パリティの原則(セクション20.5)に従い、ドキュメントではなく実際の挙動に合わせる。
    backend-java/backend-kotlin/backend-rust/backend-c/backend-cppの全てで同じ既知の差異が
    確認・踏襲されている)
    """
    return {
        "id": task.id,
        "name": task.name,
        "description": task.description,
        "status": task.status.wire_value,
        "finished_on": task.finished_on.isoformat(),
        "labels": [{"id": label.id, "name": label.name} for label in task.labels],
        "created_at": task.created_at.strftime(RFC3339_FORMAT),
        "updated_at": task.updated_at.strftime(RFC3339_FORMAT),
    }
