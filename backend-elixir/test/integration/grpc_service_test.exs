defmodule BackendElixir.Grpc.TaskServiceTest do
  @moduledoc """
  実DB+実gRPCサーバー(テスト専用ポート)を使った結合テスト。生成された`Task.V1.TaskService.Stub`を
  使い、実際にRPCを送る(backend-java/backend-kotlin/backend-pythonのgrpc結合テストと同じ設計)
  """
  use ExUnit.Case, async: false
  @moduletag :integration

  alias BackendElixir.Auth.{Dispatcher, HmacVerifier}
  alias BackendElixir.TestSupport.{DbFixture, TestTokenHelper}

  @test_port 19104
  @hmac_secret "test-hmac-secret-for-grpc-integration"
  @audience "backend"

  setup_all do
    dispatcher =
      Dispatcher.new()
      |> Dispatcher.register(
        Dispatcher.local_hmac_issuer(),
        HmacVerifier,
        HmacVerifier.new(@hmac_secret, Dispatcher.local_hmac_issuer(), @audience)
      )

    Application.put_env(:backend_elixir, :dispatcher, dispatcher)

    # テストプロセス自身がgRPCクライアントとしてサーバーへ接続するため、
    # 本番では不要なGRPC.Client.Supervisorをテストでのみ明示的に起動する
    start_supervised!({GRPC.Client.Supervisor, []})

    start_supervised!(
      {GRPC.Server.Supervisor, endpoint: BackendElixir.Grpc.Endpoint, port: @test_port, start_server: true}
    )

    {:ok, channel} = GRPC.Stub.connect("127.0.0.1:#{@test_port}")
    %{channel: channel}
  end

  setup do
    suffix = DbFixture.unique_suffix()
    user_id = DbFixture.create_user(suffix)
    on_exit(fn -> DbFixture.cleanup_user(user_id) end)
    token = TestTokenHelper.make_hmac_token(@hmac_secret, Dispatcher.local_hmac_issuer(), @audience, to_string(user_id), 3600)
    %{user_id: user_id, token: token}
  end

  defp auth_opts(token), do: [metadata: %{"authorization" => "Bearer " <> token}]

  test "full crud round trip", %{channel: channel, token: token} do
    create_req = %Task.V1.CreateTaskRequest{name: "grpc-task", status: "waiting", finished_on: "2099-01-01", label_ids: []}
    {:ok, created} = Task.V1.TaskService.Stub.create_task(channel, create_req, auth_opts(token))
    assert created.name == "grpc-task"

    {:ok, fetched} = Task.V1.TaskService.Stub.get_task(channel, %Task.V1.GetTaskRequest{id: created.id}, auth_opts(token))
    assert fetched.id == created.id

    update_req = %Task.V1.UpdateTaskRequest{
      id: created.id,
      name: "grpc-task-updated",
      status: "completed",
      finished_on: "2099-01-01",
      label_ids: []
    }

    {:ok, updated} = Task.V1.TaskService.Stub.update_task(channel, update_req, auth_opts(token))
    assert updated.name == "grpc-task-updated"
    assert updated.status == "completed"

    {:ok, _} = Task.V1.TaskService.Stub.delete_task(channel, %Task.V1.DeleteTaskRequest{id: created.id}, auth_opts(token))
    {:error, error} = Task.V1.TaskService.Stub.get_task(channel, %Task.V1.GetTaskRequest{id: created.id}, auth_opts(token))
    assert error.status == GRPC.Status.not_found()
  end

  test "delete removes task_labels rows", %{channel: channel, token: token, user_id: user_id} do
    suffix = DbFixture.unique_suffix()
    label_id = DbFixture.create_label(suffix)
    on_exit(fn -> DbFixture.cleanup_label(label_id) end)

    create_req = %Task.V1.CreateTaskRequest{
      name: "grpc-label-task",
      status: "waiting",
      finished_on: "2099-01-01",
      label_ids: [label_id]
    }

    {:ok, created} = Task.V1.TaskService.Stub.create_task(channel, create_req, auth_opts(token))
    assert DbFixture.count_task_labels(created.id) == 1

    {:ok, _} = Task.V1.TaskService.Stub.delete_task(channel, %Task.V1.DeleteTaskRequest{id: created.id}, auth_opts(token))
    assert DbFixture.count_task_labels(created.id) == 0
    # user_idはfixtureのcleanup対象として既にon_exitで登録済み(setup参照)、ここでは参照のみ
    assert is_integer(user_id)
  end

  test "unauthenticated call is rejected", %{channel: channel} do
    {:error, error} = Task.V1.TaskService.Stub.list_tasks(channel, %Task.V1.ListTasksRequest{limit: 1}, [])
    assert error.status == GRPC.Status.unauthenticated()
  end

  test "expired token is rejected", %{channel: channel, user_id: user_id} do
    expired_token =
      TestTokenHelper.make_hmac_token(@hmac_secret, Dispatcher.local_hmac_issuer(), @audience, to_string(user_id), -3600)

    {:error, error} = Task.V1.TaskService.Stub.list_tasks(channel, %Task.V1.ListTasksRequest{limit: 1}, auth_opts(expired_token))
    assert error.status == GRPC.Status.unauthenticated()
  end

  test "list_tasks cursor pagination chains to no next page", %{channel: channel, token: token} do
    for i <- 1..3 do
      req = %Task.V1.CreateTaskRequest{name: "cursor-task-#{i}", status: "waiting", finished_on: "2099-01-01", label_ids: []}
      {:ok, _} = Task.V1.TaskService.Stub.create_task(channel, req, auth_opts(token))
    end

    {:ok, page1} = Task.V1.TaskService.Stub.list_tasks(channel, %Task.V1.ListTasksRequest{limit: 2}, auth_opts(token))
    assert length(page1.tasks) == 2
    assert page1.next_cursor > 0

    {:ok, page2} =
      Task.V1.TaskService.Stub.list_tasks(channel, %Task.V1.ListTasksRequest{cursor: page1.next_cursor, limit: 2}, auth_opts(token))

    assert length(page2.tasks) == 1
    assert page2.next_cursor == 0
  end
end
