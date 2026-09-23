from __future__ import annotations

import grpc
import grpc.aio
import pytest

from app import generated_path  # noqa: F401
from app.auth.dispatcher import LOCAL_HMAC_ISSUER, Dispatcher
from app.auth.hmac_verifier import HmacVerifier
from app.auth.user_resolver import UserResolver
from app.grpc_.service import TaskGrpcService
from app.repository.task_repository import TaskRepository
from task.v1 import task_pb2, task_pb2_grpc
from tests.integration.db_fixture import DbTestFixture, build_test_pool
from tests.unit.support.test_token_helper import make_hmac_token

pytestmark = pytest.mark.integration

HMAC_SECRET = "test-secret-at-least-32-bytes-long!!"
TEST_PORT = 19103


@pytest.fixture
async def db_pool():
    pool = await build_test_pool()
    yield pool
    pool.close()
    await pool.wait_closed()


@pytest.fixture
async def grpc_server(db_pool):
    """実DB(docker-compose上のMySQL)+実gRPCサーバーに対する結合テスト。
    grpc.aioの生成スタブ(TaskServiceStub)をそのまま使ってRPCを送る
    (backend-c/backend-cppの手書きgRPCクライアントと違い、Pythonでも正規のクライアントコードで
    そのまま検証できる、backend-kotlinのTaskServiceCoroutineStubと同じ立ち位置)。

    【実機検証で判明】pytest-asyncioは既定でテスト関数ごとに新しいイベントループを使うため、
    gRPCチャンネル/サーバー/DBコネクションプールのようにイベントループに紐づくオブジェクトを
    module/sessionスコープのfixtureで共有すると、「別のループに属するFutureを待っている」
    というRuntimeErrorになる(実際に再現した)。このため全fixtureを関数スコープ(既定)にし、
    テストごとに新しいサーバー/プール/チャンネルを作る設計にした
    """
    repository = TaskRepository(db_pool)
    dispatcher = Dispatcher().register(LOCAL_HMAC_ISSUER, HmacVerifier(HMAC_SECRET, LOCAL_HMAC_ISSUER, "backend"))
    user_resolver = UserResolver(dispatcher, repository)
    service = TaskGrpcService(repository, user_resolver)

    server = grpc.aio.server()
    task_pb2_grpc.add_TaskServiceServicer_to_server(service, server)
    server.add_insecure_port(f"127.0.0.1:{TEST_PORT}")
    await server.start()
    yield server
    await server.stop(None)


@pytest.fixture
async def channel(grpc_server):
    ch = grpc.aio.insecure_channel(f"127.0.0.1:{TEST_PORT}")
    yield ch
    await ch.close()


@pytest.fixture
async def fixture(db_pool):
    return DbTestFixture(db_pool)


@pytest.fixture
async def user_id(fixture: DbTestFixture):
    uid = await fixture.create_user(f"grpc-{fixture.unique_suffix()}")
    yield uid
    await fixture.cleanup_user(uid)


def _stub_with_token(channel, user_id: int) -> task_pb2_grpc.TaskServiceStub:
    token = make_hmac_token(HMAC_SECRET, LOCAL_HMAC_ISSUER, "backend", str(user_id), 3600)
    stub = task_pb2_grpc.TaskServiceStub(channel)
    return stub, [("authorization", f"Bearer {token}")]


async def test_full_crud_round_trip(channel, user_id: int):
    stub, metadata = _stub_with_token(channel, user_id)

    created = await stub.CreateTask(
        task_pb2.CreateTaskRequest(name="grpc task", status="waiting", finished_on="2099-01-01"),
        metadata=metadata,
    )
    assert created.name == "grpc task"

    fetched = await stub.GetTask(task_pb2.GetTaskRequest(id=created.id), metadata=metadata)
    assert fetched.id == created.id

    updated = await stub.UpdateTask(
        task_pb2.UpdateTaskRequest(id=created.id, name="grpc task updated", status="completed", finished_on="2099-01-01"),
        metadata=metadata,
    )
    assert updated.name == "grpc task updated"
    assert updated.status == "completed"

    await stub.DeleteTask(task_pb2.DeleteTaskRequest(id=created.id), metadata=metadata)

    with pytest.raises(grpc.aio.AioRpcError) as exc_info:
        await stub.GetTask(task_pb2.GetTaskRequest(id=created.id), metadata=metadata)
    assert exc_info.value.code() == grpc.StatusCode.NOT_FOUND


async def test_delete_removes_task_labels_rows(channel, user_id: int, fixture: DbTestFixture):
    stub, metadata = _stub_with_token(channel, user_id)
    label_id = await fixture.create_label(f"grpc-label-{fixture.unique_suffix()}")
    try:
        created = await stub.CreateTask(
            task_pb2.CreateTaskRequest(name="grpc labels test", status="waiting", finished_on="2099-01-01", label_ids=[label_id]),
            metadata=metadata,
        )
        assert await fixture.count_task_labels(created.id) == 1

        await stub.DeleteTask(task_pb2.DeleteTaskRequest(id=created.id), metadata=metadata)
        assert await fixture.count_task_labels(created.id) == 0
    finally:
        await fixture.cleanup_label(label_id)


async def test_unauthenticated_call_is_rejected(channel):
    stub = task_pb2_grpc.TaskServiceStub(channel)
    with pytest.raises(grpc.aio.AioRpcError) as exc_info:
        await stub.ListTasks(task_pb2.ListTasksRequest(limit=1))
    assert exc_info.value.code() == grpc.StatusCode.UNAUTHENTICATED


async def test_expired_token_is_rejected(channel, user_id: int):
    expired_token = make_hmac_token(HMAC_SECRET, LOCAL_HMAC_ISSUER, "backend", str(user_id), -3600)
    stub = task_pb2_grpc.TaskServiceStub(channel)
    with pytest.raises(grpc.aio.AioRpcError) as exc_info:
        await stub.ListTasks(
            task_pb2.ListTasksRequest(limit=1), metadata=[("authorization", f"Bearer {expired_token}")]
        )
    assert exc_info.value.code() == grpc.StatusCode.UNAUTHENTICATED


async def test_list_tasks_cursor_pagination_chains_to_no_next_page(channel, user_id: int):
    stub, metadata = _stub_with_token(channel, user_id)

    id1 = (
        await stub.CreateTask(
            task_pb2.CreateTaskRequest(name="c1", status="waiting", finished_on="2099-01-01"), metadata=metadata
        )
    ).id
    id2 = (
        await stub.CreateTask(
            task_pb2.CreateTaskRequest(name="c2", status="waiting", finished_on="2099-01-01"), metadata=metadata
        )
    ).id
    id3 = (
        await stub.CreateTask(
            task_pb2.CreateTaskRequest(name="c3", status="waiting", finished_on="2099-01-01"), metadata=metadata
        )
    ).id

    first_page = await stub.ListTasks(task_pb2.ListTasksRequest(limit=2), metadata=metadata)
    assert len(first_page.tasks) == 2
    assert first_page.next_cursor == id2

    second_page = await stub.ListTasks(
        task_pb2.ListTasksRequest(limit=2, cursor=first_page.next_cursor), metadata=metadata
    )
    assert len(second_page.tasks) == 1
    assert second_page.next_cursor == 0
    assert second_page.tasks[0].id == id3
