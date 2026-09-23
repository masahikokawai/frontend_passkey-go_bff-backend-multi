defmodule BackendElixir.Domain.ValidationTest do
  use ExUnit.Case, async: true

  alias BackendElixir.Domain.{TaskInput, TaskStatus, Validation}

  @today ~D[2026-06-15]

  defp input(overrides \\ %{}) do
    base = %TaskInput{name: "test", description: nil, status_raw: "waiting", finished_on: @today, label_ids: []}
    struct(base, overrides)
  end

  test "accepts valid input" do
    assert {:ok, 1, "waiting"} = Validation.validate_task_input(input(), @today)
  end

  test "rejects empty name" do
    assert {:error, %{kind: :validation}} = Validation.validate_task_input(input(%{name: ""}), @today)
  end

  test "rejects name over 20 codepoints" do
    name = String.duplicate("a", 21)
    assert {:error, %{kind: :validation}} = Validation.validate_task_input(input(%{name: name}), @today)
  end

  test "accepts name exactly 20 codepoints" do
    name = String.duplicate("a", 20)
    assert {:ok, _, _} = Validation.validate_task_input(input(%{name: name}), @today)
  end

  test "accepts astral emoji name of 20 codepoints" do
    # 😀(U+1F600)は基本多言語面外の1コードポイント。Elixirは`String.codepoints/1`で
    # 明示的にコードポイント数を数えるため、UTF-8のバイト数(4バイト/文字)には影響されない
    name = String.duplicate("😀", 20)
    assert {:ok, _, _} = Validation.validate_task_input(input(%{name: name}), @today)
  end

  test "rejects astral emoji name of 21 codepoints" do
    name = String.duplicate("😀", 21)
    assert {:error, %{kind: :validation}} = Validation.validate_task_input(input(%{name: name}), @today)
  end

  test "rejects past finished_on" do
    yesterday = Date.add(@today, -1)
    assert {:error, %{kind: :validation}} = Validation.validate_task_input(input(%{finished_on: yesterday}), @today)
  end

  test "accepts finished_on equal to today" do
    assert {:ok, _, _} = Validation.validate_task_input(input(%{finished_on: @today}), @today)
  end

  test "rejects unknown status" do
    assert {:error, %{kind: :validation}} = Validation.validate_task_input(input(%{status_raw: "bogus"}), @today)
  end

  test "status round trips through wire and db value" do
    assert {:ok, 1, "waiting"} = TaskStatus.from_wire_value("waiting")
    assert {:ok, 2, "work_in_progress"} = TaskStatus.from_wire_value("work_in_progress")
    assert {:ok, 3, "completed"} = TaskStatus.from_wire_value("completed")
    assert {:ok, 1, "waiting"} = TaskStatus.from_db_value(1)
    assert {:ok, 2, "work_in_progress"} = TaskStatus.from_db_value(2)
    assert {:ok, 3, "completed"} = TaskStatus.from_db_value(3)
    assert :error = TaskStatus.from_wire_value("bogus")
    assert :error = TaskStatus.from_db_value(99)
  end

  test "parse_finished_on rejects calendar-invalid dates" do
    assert {:error, %{kind: :invalid_finished_on}} = Validation.parse_finished_on("2026-02-30")
    assert {:ok, ~D[2024-02-29]} = Validation.parse_finished_on("2024-02-29")
  end
end
