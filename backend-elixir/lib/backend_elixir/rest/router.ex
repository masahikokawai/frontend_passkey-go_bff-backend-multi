defmodule BackendElixir.Rest.Router do
  @moduledoc """
  内部REST v1(:8117)。`Plug.Router`のルーティングマクロで明示的にルートを登録する
  (Phoenixのコントローラ/ジェネレータは使わない、README.md「アーキテクチャ選定」節参照)
  """

  use Plug.Router
  require Logger

  alias BackendElixir.Auth.UserResolver
  alias BackendElixir.Domain.{TaskError, TaskInput, Validation}
  alias BackendElixir.Repository.TaskRepository
  alias BackendElixir.Rest.{ErrorMapper, RequestLogger, TaskJson}

  plug RequestLogger, label: "rest"
  plug Plug.Parsers, parsers: [:json], json_decoder: Jason, pass: ["*/*"]
  plug :fetch_query_params
  plug :match
  plug :dispatch

  get "/internal/v1/tasks" do
    with {:ok, user_id} <- authenticate(conn) do
      limit = query_int(conn, "limit", 20)
      offset = query_int(conn, "offset", 0)
      {:ok, tasks, total} = TaskRepository.list_offset(user_id, limit, offset)

      send_json(conn, 200, %{
        tasks: Enum.map(tasks, &TaskJson.to_json/1),
        total: total,
        limit: limit,
        offset: offset
      })
    else
      {:error, error} -> send_error(conn, error)
    end
  end

  get "/internal/v1/tasks/:id" do
    with {:ok, user_id} <- authenticate(conn),
         {:ok, task_id} <- parse_id(id),
         {:ok, task} <- TaskRepository.find_by_id(task_id, user_id) do
      send_json(conn, 200, TaskJson.to_json(task))
    else
      {:error, error} -> send_error(conn, error)
    end
  end

  post "/internal/v1/tasks" do
    with {:ok, user_id} <- authenticate(conn),
         {:ok, input} <- parse_body(conn),
         {:ok, status_db, _status_wire} <- Validation.validate_task_input(input, Date.utc_today()),
         {:ok, task_id} <- TaskRepository.create(user_id, input, status_db),
         {:ok, task} <- TaskRepository.find_by_id(task_id, user_id) do
      send_json(conn, 201, TaskJson.to_json(task))
    else
      {:error, error} -> send_error(conn, error)
    end
  end

  patch "/internal/v1/tasks/:id" do
    with {:ok, user_id} <- authenticate(conn),
         {:ok, task_id} <- parse_id(id),
         {:ok, input} <- parse_body(conn),
         {:ok, status_db, _status_wire} <- Validation.validate_task_input(input, Date.utc_today()),
         {:ok, true} <- TaskRepository.update(task_id, user_id, input, status_db),
         {:ok, task} <- TaskRepository.find_by_id(task_id, user_id) do
      send_json(conn, 200, TaskJson.to_json(task))
    else
      {:ok, false} -> send_error(conn, TaskError.not_found())
      {:error, error} -> send_error(conn, error)
    end
  end

  delete "/internal/v1/tasks/:id" do
    with {:ok, user_id} <- authenticate(conn),
         {:ok, task_id} <- parse_id(id),
         {:ok, true} <- TaskRepository.delete(task_id, user_id) do
      send_resp(conn, 204, "")
    else
      {:ok, false} -> send_error(conn, TaskError.not_found())
      {:error, error} -> send_error(conn, error)
    end
  end

  match _ do
    send_error(conn, TaskError.invalid_request())
  end

  defp authenticate(conn) do
    dispatcher = Application.fetch_env!(:backend_elixir, :dispatcher)
    auth_header = conn |> get_req_header("authorization") |> List.first()

    case UserResolver.resolve(dispatcher, auth_header) do
      {:ok, user_id} = ok ->
        Logger.debug(fn -> "rest auth resolved user_id=#{user_id} path=#{conn.request_path}" end)
        ok

      {:error, _} = err ->
        err
    end
  end

  defp parse_id(raw) do
    case Integer.parse(raw) do
      {id, ""} -> {:ok, id}
      _ -> {:error, TaskError.invalid_id()}
    end
  end

  # backend(Go)のtaskRequestBody(binding:"required")と同じ「空文字も必須違反」の扱い
  defp parse_body(conn) do
    params = conn.body_params

    name = Map.get(params, "name")
    status_raw = Map.get(params, "status")
    finished_on_raw = Map.get(params, "finished_on")

    if blank?(name) or blank?(status_raw) or blank?(finished_on_raw) do
      {:error, TaskError.invalid_request()}
    else
      with {:ok, finished_on} <- Validation.parse_finished_on(finished_on_raw) do
        {:ok,
         %TaskInput{
           name: name,
           description: Map.get(params, "description"),
           status_raw: status_raw,
           finished_on: finished_on,
           label_ids: Map.get(params, "label_ids", [])
         }}
      end
    end
  end

  defp blank?(nil), do: true
  defp blank?(""), do: true
  defp blank?(_), do: false

  defp query_int(conn, key, default) do
    case conn.query_params[key] do
      nil -> default
      raw -> String.to_integer(raw)
    end
  end

  defp send_json(conn, status, body) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(body))
  end

  defp send_error(conn, error) do
    {status, body} = ErrorMapper.status_and_body(error)
    send_json(conn, status, body)
  end
end
