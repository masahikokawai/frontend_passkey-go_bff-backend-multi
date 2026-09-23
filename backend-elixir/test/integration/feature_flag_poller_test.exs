defmodule BackendElixir.Flags.FeatureFlagPollerTest do
  @moduledoc """
  実DB結合テスト。`backend.external-tasks-pagination-v2`(全言語で共有するフラグ)には触れず、
  テスト専用の一意なflag_keyを自分で挿入・削除して検証する
  """
  use ExUnit.Case, async: false
  @moduletag :integration

  alias BackendElixir.Flags.FeatureFlagPoller
  alias BackendElixir.Repo

  setup do
    suffix = "#{System.system_time(:microsecond)}"
    flag_key = "backend-elixir-test-flag-#{suffix}"
    {:ok, pid} = FeatureFlagPoller.start_link(name: :"poller_test_#{suffix}")
    on_exit(fn -> delete_flag(flag_key) end)
    %{flag_key: flag_key, pid: pid}
  end

  defp insert_flag(flag_key, enabled, default_variation) do
    now = DateTime.utc_now() |> DateTime.to_naive() |> NaiveDateTime.truncate(:second)

    Ecto.Adapters.SQL.query(
      Repo,
      "INSERT INTO feature_flags (flag_key, description, default_variation, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
      [flag_key, "test", default_variation, enabled, now, now]
    )
  end

  defp delete_flag(flag_key) do
    Ecto.Adapters.SQL.query(Repo, "DELETE FROM feature_flags WHERE flag_key = ?", [flag_key])
  rescue
    _ -> :ok
  end

  test "returns the default_variation when enabled and found", %{flag_key: flag_key, pid: pid} do
    {:ok, _} = insert_flag(flag_key, 1, "on")
    :ok = FeatureFlagPoller.refresh(pid)

    assert FeatureFlagPoller.variation(pid, flag_key, "off") == "on"
  end

  test "returns the caller default when disabled", %{flag_key: flag_key, pid: pid} do
    {:ok, _} = insert_flag(flag_key, 0, "on")
    :ok = FeatureFlagPoller.refresh(pid)

    assert FeatureFlagPoller.variation(pid, flag_key, "off") == "off"
  end

  test "returns the caller default when the flag_key does not exist", %{flag_key: flag_key, pid: pid} do
    :ok = FeatureFlagPoller.refresh(pid)
    assert FeatureFlagPoller.variation(pid, flag_key, "off") == "off"
  end

  test "reflects a real DB flip within one manual poll cycle", %{flag_key: flag_key, pid: pid} do
    {:ok, _} = insert_flag(flag_key, 1, "off")
    :ok = FeatureFlagPoller.refresh(pid)
    assert FeatureFlagPoller.variation(pid, flag_key, "unset") == "off"

    Ecto.Adapters.SQL.query(Repo, "UPDATE feature_flags SET default_variation = 'on' WHERE flag_key = ?", [flag_key])
    :ok = FeatureFlagPoller.refresh(pid)
    assert FeatureFlagPoller.variation(pid, flag_key, "unset") == "on"
  end
end
