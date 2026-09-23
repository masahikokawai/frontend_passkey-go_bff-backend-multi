from __future__ import annotations

from datetime import date

import pytest

from app.domain.models import TaskInput, TaskStatus
from app.repository.task_repository import TaskRepository
from tests.integration.db_fixture import DbTestFixture, build_test_pool

pytestmark = pytest.mark.integration


@pytest.fixture
async def fixture():
    pool = await build_test_pool()
    fx = DbTestFixture(pool)
    yield fx
    pool.close()
    await pool.wait_closed()


@pytest.fixture
async def repository(fixture: DbTestFixture) -> TaskRepository:
    return TaskRepository(fixture.pool)


@pytest.fixture
async def user_id(fixture: DbTestFixture):
    suffix = fixture.unique_suffix()
    uid = await fixture.create_user(suffix)
    yield uid
    await fixture.cleanup_user(uid)


@pytest.fixture
async def label_ids(fixture: DbTestFixture):
    suffix = fixture.unique_suffix()
    a = await fixture.create_label(f"{suffix}-a")
    b = await fixture.create_label(f"{suffix}-b")
    yield a, b
    await fixture.cleanup_label(a)
    await fixture.cleanup_label(b)


async def test_create_find_update_delete_round_trip(repository: TaskRepository, user_id: int, label_ids):
    label_a, label_b = label_ids
    input_ = TaskInput("task A", "desc", "waiting", date(2099, 1, 1), [label_a, label_b])
    task_id = await repository.create(user_id, input_, TaskStatus.WAITING)

    created = await repository.find_by_id(task_id, user_id)
    assert created is not None
    assert created.name == "task A"
    assert created.description == "desc"
    assert created.status == TaskStatus.WAITING
    assert created.finished_on == date(2099, 1, 1)
    assert len(created.labels) == 2

    update_input = TaskInput("task A updated", None, "completed", date(2099, 2, 2), [label_b])
    updated = await repository.update(task_id, user_id, update_input, TaskStatus.COMPLETED)
    assert updated is True

    after_update = await repository.find_by_id(task_id, user_id)
    assert after_update is not None
    assert after_update.name == "task A updated"
    assert after_update.description is None
    assert after_update.status == TaskStatus.COMPLETED
    assert len(after_update.labels) == 1

    deleted = await repository.delete(task_id, user_id)
    assert deleted is True
    assert await repository.find_by_id(task_id, user_id) is None


async def test_update_of_nonexistent_task_returns_false(repository: TaskRepository, user_id: int):
    input_ = TaskInput("x", None, "waiting", date(2099, 1, 1), [])
    updated = await repository.update(999_999_999, user_id, input_, TaskStatus.WAITING)
    assert updated is False


async def test_delete_of_nonexistent_task_returns_false(repository: TaskRepository, user_id: int):
    deleted = await repository.delete(999_999_999, user_id)
    assert deleted is False


async def test_delete_removes_task_labels_rows(
    repository: TaskRepository, user_id: int, label_ids, fixture: DbTestFixture
):
    """tasksとtask_labelsの削除が1つのトランザクションで包まれていることを確認する
    (task_labelsに外部キー制約が無いため、これが無いと孤立行が残り得る既知バグクラス、
    backend-rustで実際に見つかった経緯がある)
    """
    label_a, label_b = label_ids
    input_ = TaskInput("task with labels", None, "waiting", date(2099, 1, 1), [label_a, label_b])
    task_id = await repository.create(user_id, input_, TaskStatus.WAITING)

    assert await fixture.count_task_labels(task_id) == 2
    await repository.delete(task_id, user_id)
    assert await fixture.count_task_labels(task_id) == 0


async def test_create_dedups_duplicate_label_ids(
    repository: TaskRepository, user_id: int, label_ids, fixture: DbTestFixture
):
    label_a, label_b = label_ids
    input_ = TaskInput("dedup test", None, "waiting", date(2099, 1, 1), [label_a, label_a, label_b])
    task_id = await repository.create(user_id, input_, TaskStatus.WAITING)

    task = await repository.find_by_id(task_id, user_id)
    assert task is not None
    assert len(task.labels) == 2
    assert await fixture.count_task_labels(task_id) == 2


async def test_update_dedups_duplicate_label_ids(
    repository: TaskRepository, user_id: int, label_ids, fixture: DbTestFixture
):
    label_a, label_b = label_ids
    input_ = TaskInput("dedup update test", None, "waiting", date(2099, 1, 1), [])
    task_id = await repository.create(user_id, input_, TaskStatus.WAITING)

    update_input = TaskInput("dedup update test", None, "waiting", date(2099, 1, 1), [label_b, label_b, label_a])
    await repository.update(task_id, user_id, update_input, TaskStatus.WAITING)

    assert await fixture.count_task_labels(task_id) == 2


async def test_other_user_cannot_see_task(repository: TaskRepository, user_id: int, fixture: DbTestFixture):
    suffix = f"{fixture.unique_suffix()}-other"
    other_user_id = await fixture.create_user(suffix)
    try:
        input_ = TaskInput("private task", None, "waiting", date(2099, 1, 1), [])
        task_id = await repository.create(user_id, input_, TaskStatus.WAITING)

        assert await repository.find_by_id(task_id, other_user_id) is None
        assert await repository.find_by_id(task_id, user_id) is not None
    finally:
        await fixture.cleanup_user(other_user_id)


async def test_list_offset_returns_total_and_respects_limit_offset(repository: TaskRepository, user_id: int):
    for i in range(3):
        input_ = TaskInput(f"offset-test-{i}", None, "waiting", date(2099, 1, 1), [])
        await repository.create(user_id, input_, TaskStatus.WAITING)

    page1 = await repository.list_offset(user_id, 2, 0)
    assert page1.total == 3
    assert len(page1.tasks) == 2

    page2 = await repository.list_offset(user_id, 2, 2)
    assert page2.total == 3
    assert len(page2.tasks) == 1


async def test_list_cursor_orders_by_id_ascending_and_respects_after_id(repository: TaskRepository, user_id: int):
    id1 = await repository.create(
        user_id, TaskInput("cursor-1", None, "waiting", date(2099, 1, 1), []), TaskStatus.WAITING
    )
    id2 = await repository.create(
        user_id, TaskInput("cursor-2", None, "waiting", date(2099, 1, 1), []), TaskStatus.WAITING
    )
    id3 = await repository.create(
        user_id, TaskInput("cursor-3", None, "waiting", date(2099, 1, 1), []), TaskStatus.WAITING
    )

    first_page = await repository.list_cursor(user_id, 0, 2)
    assert len(first_page) == 2
    assert first_page[0].id == id1
    assert first_page[1].id == id2

    second_page = await repository.list_cursor(user_id, id2, 2)
    assert len(second_page) == 1
    assert second_page[0].id == id3


async def test_find_user_by_id_returns_user_when_exists(repository: TaskRepository, user_id: int):
    found = await repository.find_user_by_id(user_id)
    assert found is not None
    assert found.id == user_id


async def test_find_user_by_id_returns_none_when_not_found(repository: TaskRepository):
    assert await repository.find_user_by_id(999_999_999) is None


async def test_find_user_by_keycloak_sub_returns_user_when_exists(repository: TaskRepository, fixture: DbTestFixture):
    suffix = f"kc-{fixture.unique_suffix()}"
    keycloak_sub = f"keycloak-sub-{suffix}"
    kc_user_id = await fixture.create_user_with_keycloak_sub(suffix, keycloak_sub)
    try:
        found = await repository.find_user_by_keycloak_sub(keycloak_sub)
        assert found is not None
        assert found.id == kc_user_id
    finally:
        await fixture.cleanup_user(kc_user_id)


async def test_find_user_by_keycloak_sub_returns_none_when_not_found(repository: TaskRepository):
    assert await repository.find_user_by_keycloak_sub("no-such-keycloak-sub") is None
