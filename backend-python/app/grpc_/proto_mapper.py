from __future__ import annotations

from datetime import datetime

from google.protobuf.timestamp_pb2 import Timestamp

from app import generated_path  # noqa: F401
from app.domain.errors import TaskError
from app.domain.models import Task, TaskInput
from app.domain.validation import parse_finished_on
from task.v1 import task_pb2


def to_proto(task: Task) -> task_pb2.Task:
    proto_task = task_pb2.Task(
        id=task.id,
        name=task.name,
        status=task.status.wire_value,
        finished_on=task.finished_on.isoformat(),
        labels=[task_pb2.Label(id=label.id, name=label.name) for label in task.labels],
        created_at=_to_timestamp(task.created_at),
        updated_at=_to_timestamp(task.updated_at),
    )
    if task.description is not None:
        proto_task.description = task.description
    return proto_task


def from_create_request(req: task_pb2.CreateTaskRequest) -> TaskInput:
    finished_on = parse_finished_on(req.finished_on)
    description = req.description if req.HasField("description") else None
    return TaskInput(
        name=req.name,
        description=description,
        status_raw=req.status,
        finished_on=finished_on,
        label_ids=list(req.label_ids),
    )


def from_update_request(req: task_pb2.UpdateTaskRequest) -> TaskInput:
    finished_on = parse_finished_on(req.finished_on)
    description = req.description if req.HasField("description") else None
    return TaskInput(
        name=req.name,
        description=description,
        status_raw=req.status,
        finished_on=finished_on,
        label_ids=list(req.label_ids),
    )


def _to_timestamp(dt: datetime) -> Timestamp:
    # 【実機検証で確認済み】Timestamp#FromDatetime()はナイーブなdatetimeをUTCとして扱う
    # (システムのローカルタイムゾーンを経由した変換は行われない)。aiomysqlが返す
    # ナイーブなdatetimeは元々UTCの壁時計値そのものであるため、追加の変換は一切不要
    # (README.md「実装時に判明した既知の差異」参照)
    ts = Timestamp()
    ts.FromDatetime(dt)
    return ts
