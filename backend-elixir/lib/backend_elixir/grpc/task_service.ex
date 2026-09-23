defmodule BackendElixir.Grpc.TaskService do
  @moduledoc """
  内部gRPC v2(:9104)。REST v1と同じRepository・ドメインモデル・認証(Dispatcher/UserResolver)を
  共有する(認証・バリデーション・トランザクション保護のロジックを複製しない設計、
  backend-java/backend-kotlin/backend-pythonのgrpcサービスと同じ方針)。

  【Elixir固有の簡略化】grpc-java/grpc-kotlinは認証ヘッダをスレッド/コルーチンを跨いで伝播する
  ために専用のインターセプタ機構を必要とした。grpc-elixirは`stream.http_request_headers`という
  マップから直接メタデータを読めるため、各RPC関数から素朴に読み取ればよい(backend-pythonの
  `context.invocation_metadata()`と同じ簡略さ)
  """

  use GRPC.Server, service: Task.V1.TaskService.Service

  require Logger

  alias BackendElixir.Auth.UserResolver
  alias BackendElixir.Domain.{TaskError, Validation}
  alias BackendElixir.Grpc.ProtoMapper
  alias BackendElixir.Repository.TaskRepository

  def list_tasks(req, stream) do
    logged("list_tasks", stream, fn ->
      with {:ok, user_id} <- authenticate(stream) do
        limit = if req.limit > 0, do: req.limit, else: 20
        {:ok, tasks} = TaskRepository.list_cursor(user_id, req.cursor, limit)
        next_cursor = if tasks == [] or length(tasks) < limit, do: 0, else: List.last(tasks).id
        {:ok, %Task.V1.ListTasksResponse{tasks: Enum.map(tasks, &ProtoMapper.to_proto/1), next_cursor: next_cursor}}
      end
    end)
  end

  def get_task(req, stream) do
    logged("get_task", stream, fn ->
      with {:ok, user_id} <- authenticate(stream),
           {:ok, task} <- TaskRepository.find_by_id(req.id, user_id) do
        {:ok, ProtoMapper.to_proto(task)}
      end
    end)
  end

  def create_task(req, stream) do
    logged("create_task", stream, fn ->
      with {:ok, user_id} <- authenticate(stream),
           {:ok, input} <- ProtoMapper.from_create_request(req),
           {:ok, status_db, _} <- Validation.validate_task_input(input, Date.utc_today()),
           {:ok, task_id} <- TaskRepository.create(user_id, input, status_db),
           {:ok, task} <- TaskRepository.find_by_id(task_id, user_id) do
        {:ok, ProtoMapper.to_proto(task)}
      end
    end)
  end

  def update_task(req, stream) do
    logged("update_task", stream, fn ->
      with {:ok, user_id} <- authenticate(stream),
           {:ok, input} <- ProtoMapper.from_update_request(req),
           {:ok, status_db, _} <- Validation.validate_task_input(input, Date.utc_today()),
           {:ok, true} <- TaskRepository.update(req.id, user_id, input, status_db),
           {:ok, task} <- TaskRepository.find_by_id(req.id, user_id) do
        {:ok, ProtoMapper.to_proto(task)}
      else
        {:ok, false} -> {:error, TaskError.not_found()}
        other -> other
      end
    end)
  end

  def delete_task(req, stream) do
    logged("delete_task", stream, fn ->
      with {:ok, user_id} <- authenticate(stream),
           {:ok, true} <- TaskRepository.delete(req.id, user_id) do
        {:ok, %Task.V1.DeleteTaskResponse{}}
      else
        {:ok, false} -> {:error, TaskError.not_found()}
        other -> other
      end
    end)
  end

  defp authenticate(stream) do
    dispatcher = Application.fetch_env!(:backend_elixir, :dispatcher)
    auth_header = Map.get(stream.http_request_headers, "authorization")
    UserResolver.resolve(dispatcher, auth_header)
  end

  # method/実際のgRPCステータス(成否)/durationを1rpc1行のログとして出す
  # (backend-java/backend-kotlin/backend-pythonのgrpcログ、backend-c/backend-cppの
  # grpc method=...と同じ形式)
  defp logged(method, _stream, block) do
    start = System.monotonic_time(:millisecond)

    case block.() do
      {:ok, response} ->
        log_result(method, start, "OK")
        response

      {:error, %TaskError{} = error} ->
        code = kind_to_grpc_status(error.kind)
        log_result(method, start, code)
        raise GRPC.RPCError, status: code, message: error.message
    end
  end

  defp log_result(method, start, status) do
    duration_ms = System.monotonic_time(:millisecond) - start
    Logger.info("grpc method=#{method} status=#{status} duration_ms=#{duration_ms}")
  end

  defp kind_to_grpc_status(:unauthorized), do: :unauthenticated
  defp kind_to_grpc_status(:user_not_provisioned), do: :permission_denied
  defp kind_to_grpc_status(:not_found), do: :not_found
  defp kind_to_grpc_status(:invalid_request), do: :invalid_argument
  defp kind_to_grpc_status(:invalid_id), do: :invalid_argument
  defp kind_to_grpc_status(:invalid_status), do: :invalid_argument
  defp kind_to_grpc_status(:invalid_finished_on), do: :invalid_argument
  defp kind_to_grpc_status(:validation), do: :invalid_argument
  defp kind_to_grpc_status(:internal), do: :internal
end
