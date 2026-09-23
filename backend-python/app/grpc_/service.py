from __future__ import annotations

import logging
import time
from datetime import datetime, timezone

import grpc

from app import generated_path  # noqa: F401
from app.auth.user_resolver import UserResolver
from app.domain.errors import TaskError, TaskErrorKind
from app.domain.validation import validate_task_input
from app.grpc_.proto_mapper import from_create_request, from_update_request, to_proto
from app.repository.task_repository import TaskRepository
from task.v1 import task_pb2, task_pb2_grpc

log = logging.getLogger(__name__)

_KIND_TO_GRPC_CODE = {
    TaskErrorKind.UNAUTHORIZED: grpc.StatusCode.UNAUTHENTICATED,
    TaskErrorKind.USER_NOT_PROVISIONED: grpc.StatusCode.PERMISSION_DENIED,
    TaskErrorKind.NOT_FOUND: grpc.StatusCode.NOT_FOUND,
    TaskErrorKind.INVALID_REQUEST: grpc.StatusCode.INVALID_ARGUMENT,
    TaskErrorKind.INVALID_ID: grpc.StatusCode.INVALID_ARGUMENT,
    TaskErrorKind.INVALID_STATUS: grpc.StatusCode.INVALID_ARGUMENT,
    TaskErrorKind.INVALID_FINISHED_ON: grpc.StatusCode.INVALID_ARGUMENT,
    TaskErrorKind.VALIDATION: grpc.StatusCode.INVALID_ARGUMENT,
    TaskErrorKind.INTERNAL: grpc.StatusCode.INTERNAL,
}


class TaskGrpcService(task_pb2_grpc.TaskServiceServicer):
    """内部gRPC v2(:9103)。REST v1と同じRepository・ドメインモデル・認証(Dispatcher/UserResolver)を
    共有する(認証・バリデーション・トランザクション保護のロジックを複製しない設計、
    backend-java/backend-kotlin/backend-rustのgrpcサービスと同じ方針)。

    【Java/Kotlinとの対比、Python固有の簡略化】grpc-java/grpc-kotlinは、認証ヘッダをコルーチン/
    スレッドを跨いで伝播するために専用のServerInterceptor(io.grpc.Context経由)を必要とした。
    grpc.aioの非同期サービサーメソッドは`context.invocation_metadata()`で直接メタデータへ
    アクセスできるため、この種のブリッジ用インターセプタは不要で、各RPCメソッドから
    直接読み取ればよい
    """

    def __init__(self, repository: TaskRepository, user_resolver: UserResolver) -> None:
        self._repository = repository
        self._user_resolver = user_resolver

    async def ListTasks(self, request: task_pb2.ListTasksRequest, context) -> task_pb2.ListTasksResponse:
        async def body():
            user_id = await self._authenticate(context)
            limit = request.limit if request.limit > 0 else 20
            tasks = await self._repository.list_cursor(user_id, request.cursor, limit)
            next_cursor = 0 if len(tasks) < limit or not tasks else tasks[-1].id
            return task_pb2.ListTasksResponse(tasks=[to_proto(t) for t in tasks], next_cursor=next_cursor)

        return await self._logged("list_tasks", context, body)

    async def GetTask(self, request: task_pb2.GetTaskRequest, context) -> task_pb2.Task:
        async def body():
            user_id = await self._authenticate(context)
            task = await self._repository.find_by_id(request.id, user_id)
            if task is None:
                raise TaskError.not_found()
            return to_proto(task)

        return await self._logged("get_task", context, body)

    async def CreateTask(self, request: task_pb2.CreateTaskRequest, context) -> task_pb2.Task:
        async def body():
            user_id = await self._authenticate(context)
            input_ = from_create_request(request)
            today = datetime.now(timezone.utc).date()
            status = validate_task_input(input_, today)

            task_id = await self._repository.create(user_id, input_, status)
            task = await self._repository.find_by_id(task_id, user_id)
            if task is None:
                raise TaskError.internal("task disappeared after create")
            return to_proto(task)

        return await self._logged("create_task", context, body)

    async def UpdateTask(self, request: task_pb2.UpdateTaskRequest, context) -> task_pb2.Task:
        async def body():
            user_id = await self._authenticate(context)
            input_ = from_update_request(request)
            today = datetime.now(timezone.utc).date()
            status = validate_task_input(input_, today)

            updated = await self._repository.update(request.id, user_id, input_, status)
            if not updated:
                raise TaskError.not_found()
            task = await self._repository.find_by_id(request.id, user_id)
            if task is None:
                raise TaskError.not_found()
            return to_proto(task)

        return await self._logged("update_task", context, body)

    async def DeleteTask(self, request: task_pb2.DeleteTaskRequest, context) -> task_pb2.DeleteTaskResponse:
        async def body():
            user_id = await self._authenticate(context)
            deleted = await self._repository.delete(request.id, user_id)
            if not deleted:
                raise TaskError.not_found()
            return task_pb2.DeleteTaskResponse()

        return await self._logged("delete_task", context, body)

    async def _authenticate(self, context) -> int:
        auth_header = None
        for key, value in context.invocation_metadata() or []:
            if key == "authorization":
                auth_header = value
                break
        return await self._user_resolver.resolve(auth_header)

    async def _logged(self, method: str, context, block):
        """method/実際のgRPCステータス(成否)/durationを1rpc1行のログとして出す
        (backend-java/backend-kotlin/backend-rustのgrpcログ、backend-c/backend-cppの
        grpc method=...と同じ形式)
        """
        start = time.monotonic()
        try:
            result = await block()
            duration_ms = int((time.monotonic() - start) * 1000)
            log.info("grpc method=%s status=%s duration_ms=%d", method, grpc.StatusCode.OK, duration_ms)
            return result
        except TaskError as exc:
            code = _KIND_TO_GRPC_CODE.get(exc.kind, grpc.StatusCode.INTERNAL)
            duration_ms = int((time.monotonic() - start) * 1000)
            log.info("grpc method=%s status=%s duration_ms=%d", method, code, duration_ms)
            await context.abort(code, exc.message)
