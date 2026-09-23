defmodule BackendElixir.External.Handler do
  @moduledoc """
  外部公開API(:8118、CONTRACT.mdセクション11)。BFFを経由しない唯一の例外的な経路で、
  Client Credentials Grant(Keycloak発行のみ、azpがEXTERNAL_API_CLIENT_IDと一致することを
  要求)で認証する。`backend.external-tasks-pagination-v2`フラグでoffset(v1)/cursor(v2)を
  切り替える(FeatureFlagPoller参照)。

  【既知の制約、CONTRACT.mdセクション11に明記の通り】user_idはクエリパラメータとして
  呼び出し側が指定した値をそのまま信頼する(JWTのsubから解決しない)。この資格情報を持つ
  クライアントは任意のuser_idのtaskを読み取れる、サーバー間の信頼関係を前提にした設計であり、
  エンドユーザー単位の認可は行わない
  """

  use Plug.Router
  require Logger

  alias BackendElixir.Auth.ExternalAuth
  alias BackendElixir.External.Query
  alias BackendElixir.Flags.FeatureFlagPoller
  alias BackendElixir.Repository.TaskRepository
  alias BackendElixir.Rest.{ErrorMapper, RequestLogger, TaskJson}

  plug RequestLogger, label: "external"
  plug :fetch_query_params
  plug :match
  plug :dispatch

  get "/external/v1/tasks" do
    with {:ok, _claims} <- authenticate(conn),
         {:ok, user_id} <- Query.parse_user_id(conn.query_params) do
      Logger.debug(fn ->
        "external auth resolved user_id=#{user_id} query=#{inspect(conn.query_params)}"
      end)

      if use_cursor_pagination?() do
        handle_cursor(conn, user_id)
      else
        handle_offset(conn, user_id)
      end
    else
      {:error, error} -> send_error(conn, error)
    end
  end

  match _ do
    send_error(conn, BackendElixir.Domain.TaskError.invalid_request())
  end

  defp authenticate(conn) do
    dispatcher = Application.fetch_env!(:backend_elixir, :dispatcher)
    external_api_client_id = Application.fetch_env!(:backend_elixir, :external_api_client_id)
    auth_header = conn |> get_req_header("authorization") |> List.first()
    ExternalAuth.require_external_client(dispatcher, auth_header, external_api_client_id)
  end

  defp use_cursor_pagination? do
    FeatureFlagPoller.variation("backend.external-tasks-pagination-v2", "off") == "on"
  end

  defp handle_offset(conn, user_id) do
    {page, page_size} = Query.parse_offset_page(conn.query_params)
    offset = (page - 1) * page_size
    {:ok, tasks, total} = TaskRepository.list_offset(user_id, page_size, offset)

    send_json(conn, 200, %{
      tasks: Enum.map(tasks, &TaskJson.to_json/1),
      page: page,
      page_size: page_size,
      total: total
    })
  end

  defp handle_cursor(conn, user_id) do
    {after_id, limit} = Query.parse_cursor_page(conn.query_params)
    {:ok, tasks} = TaskRepository.list_cursor(user_id, after_id, limit)
    next_cursor = if tasks == [] or length(tasks) < limit, do: nil, else: to_string(List.last(tasks).id)

    send_json(conn, 200, %{
      tasks: Enum.map(tasks, &TaskJson.to_json/1),
      next_cursor: next_cursor,
      limit: limit
    })
  end

  defp send_json(conn, status, body) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(body))
  end

  defp send_error(conn, error) do
    {status, body} = ErrorMapper.status_and_body(error)

    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(body))
  end
end
