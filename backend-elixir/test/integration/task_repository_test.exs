defmodule BackendElixir.Repository.TaskRepositoryTest do
  @moduledoc "実DB(docker-compose上のMySQL)結合テスト。テストごとに一意なuser/labelで検証する"
  use ExUnit.Case, async: false
  @moduletag :integration

  alias BackendElixir.Domain.TaskInput
  alias BackendElixir.Repository.TaskRepository
  alias BackendElixir.TestSupport.DbFixture

  setup do
    suffix = DbFixture.unique_suffix()
    user_id = DbFixture.create_user(suffix)
    on_exit(fn -> DbFixture.cleanup_user(user_id) end)
    %{user_id: user_id, suffix: suffix}
  end

  defp input(overrides \\ %{}) do
    base = %TaskInput{name: "task", description: "desc", status_raw: "waiting", finished_on: ~D[2099-01-01], label_ids: []}
    struct(base, overrides)
  end

  test "create find update delete round trip", %{user_id: user_id} do
    {:ok, task_id} = TaskRepository.create(user_id, input(), 1)
    assert {:ok, task} = TaskRepository.find_by_id(task_id, user_id)
    assert task.name == "task"
    assert task.status_wire == "waiting"

    {:ok, true} = TaskRepository.update(task_id, user_id, input(%{name: "updated", status_raw: "completed"}), 3)
    assert {:ok, updated} = TaskRepository.find_by_id(task_id, user_id)
    assert updated.name == "updated"
    assert updated.status_wire == "completed"

    assert {:ok, true} = TaskRepository.delete(task_id, user_id)
    assert {:error, %{kind: :not_found}} = TaskRepository.find_by_id(task_id, user_id)
  end

  test "update of nonexistent task returns false", %{user_id: user_id} do
    assert {:ok, false} = TaskRepository.update(999_999_999, user_id, input(), 1)
  end

  test "delete of nonexistent task returns false", %{user_id: user_id} do
    assert {:ok, false} = TaskRepository.delete(999_999_999, user_id)
  end

  test "delete removes task_labels rows", %{user_id: user_id, suffix: suffix} do
    label_id = DbFixture.create_label(suffix)
    on_exit(fn -> DbFixture.cleanup_label(label_id) end)

    {:ok, task_id} = TaskRepository.create(user_id, input(%{label_ids: [label_id]}), 1)
    assert DbFixture.count_task_labels(task_id) == 1

    assert {:ok, true} = TaskRepository.delete(task_id, user_id)
    assert DbFixture.count_task_labels(task_id) == 0
  end

  test "create dedups duplicate label ids", %{user_id: user_id, suffix: suffix} do
    label_a = DbFixture.create_label(suffix <> "-a")
    label_b = DbFixture.create_label(suffix <> "-b")
    on_exit(fn ->
      DbFixture.cleanup_label(label_a)
      DbFixture.cleanup_label(label_b)
    end)

    {:ok, task_id} = TaskRepository.create(user_id, input(%{label_ids: [label_a, label_a, label_b]}), 1)
    assert {:ok, task} = TaskRepository.find_by_id(task_id, user_id)
    assert Enum.map(task.labels, & &1.id) |> Enum.sort() == Enum.sort([label_a, label_b])
  end

  test "update dedups duplicate label ids", %{user_id: user_id, suffix: suffix} do
    label_a = DbFixture.create_label(suffix <> "-a")
    label_b = DbFixture.create_label(suffix <> "-b")
    on_exit(fn ->
      DbFixture.cleanup_label(label_a)
      DbFixture.cleanup_label(label_b)
    end)

    {:ok, task_id} = TaskRepository.create(user_id, input(), 1)
    {:ok, true} = TaskRepository.update(task_id, user_id, input(%{label_ids: [label_b, label_b, label_a]}), 1)
    assert {:ok, task} = TaskRepository.find_by_id(task_id, user_id)
    assert Enum.map(task.labels, & &1.id) |> Enum.sort() == Enum.sort([label_a, label_b])
  end

  test "other user cannot see task", %{user_id: user_id, suffix: suffix} do
    other_user_id = DbFixture.create_user(suffix <> "-other")
    on_exit(fn -> DbFixture.cleanup_user(other_user_id) end)

    {:ok, task_id} = TaskRepository.create(user_id, input(), 1)
    assert {:error, %{kind: :not_found}} = TaskRepository.find_by_id(task_id, other_user_id)
  end

  test "list_offset returns total and respects limit/offset", %{user_id: user_id} do
    for i <- 1..3, do: {:ok, _} = TaskRepository.create(user_id, input(%{name: "task-#{i}"}), 1)

    {:ok, tasks, total} = TaskRepository.list_offset(user_id, 2, 0)
    assert total == 3
    assert length(tasks) == 2

    {:ok, rest, ^total} = TaskRepository.list_offset(user_id, 2, 2)
    assert length(rest) == 1
  end

  test "list_cursor orders by id ascending and respects after_id", %{user_id: user_id} do
    {:ok, id1} = TaskRepository.create(user_id, input(%{name: "first"}), 1)
    {:ok, id2} = TaskRepository.create(user_id, input(%{name: "second"}), 1)
    {:ok, id3} = TaskRepository.create(user_id, input(%{name: "third"}), 1)

    {:ok, page1} = TaskRepository.list_cursor(user_id, 0, 2)
    assert Enum.map(page1, & &1.id) == [id1, id2]

    {:ok, page2} = TaskRepository.list_cursor(user_id, id2, 2)
    assert Enum.map(page2, & &1.id) == [id3]
  end

  test "find_user_by_id returns user when exists", %{user_id: user_id} do
    assert {:ok, user} = TaskRepository.find_user_by_id(user_id)
    assert user.id == user_id
  end

  test "find_user_by_id returns none when not found" do
    assert {:error, :not_found} = TaskRepository.find_user_by_id(999_999_999)
  end

  test "find_user_by_keycloak_sub returns user when exists", %{suffix: suffix} do
    keycloak_sub = "keycloak-sub-#{suffix}"
    user_id = DbFixture.create_user_with_keycloak_sub(suffix <> "-kc", keycloak_sub)
    on_exit(fn -> DbFixture.cleanup_user(user_id) end)

    assert {:ok, user} = TaskRepository.find_user_by_keycloak_sub(keycloak_sub)
    assert user.id == user_id
  end

  test "find_user_by_keycloak_sub returns none when not found" do
    assert {:error, :not_found} = TaskRepository.find_user_by_keycloak_sub("nonexistent-sub")
  end
end
