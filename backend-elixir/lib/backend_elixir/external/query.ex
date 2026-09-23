defmodule BackendElixir.External.Query do
  @moduledoc """
  外部公開API(GET /external/v1/tasks)のクエリパラメータ解析(ページング方式のパラメータ変換・
  境界値の丸め込み)を、HTTPハンドラから独立した単体テスト可能な関数として切り出す
  """

  alias BackendElixir.Domain.TaskError

  @doc """
  user_idクエリパラメータを解析する。空/欠如は:user_id_required、数値でなければ:invalid_user_id
  """
  def parse_user_id(params) do
    case Map.get(params, "user_id") do
      nil -> {:error, TaskError.user_id_required()}
      "" -> {:error, TaskError.user_id_required()}
      raw ->
        case Integer.parse(raw) do
          {user_id, ""} -> {:ok, user_id}
          _ -> {:error, TaskError.invalid_user_id()}
        end
    end
  end

  @doc "v1(offset)向け。page/page_sizeいずれも1未満は1に丸める"
  def parse_offset_page(params) do
    page = params |> Map.get("page") |> parse_positive_int(1)
    page_size = params |> Map.get("page_size") |> parse_positive_int(10)
    {page, page_size}
  end

  @doc "v2(cursor)向け。cursorは数値でなければ先頭からとみなす(after_id=0)。limitは1未満は1に丸める"
  def parse_cursor_page(params) do
    after_id =
      case Map.get(params, "cursor") do
        nil -> 0
        "" -> 0
        raw ->
          case Integer.parse(raw) do
            {value, ""} -> value
            _ -> 0
          end
      end

    limit = params |> Map.get("limit") |> parse_positive_int(10)
    {after_id, limit}
  end

  defp parse_positive_int(nil, default), do: default
  defp parse_positive_int("", default), do: default

  defp parse_positive_int(raw, default) do
    case Integer.parse(raw) do
      {value, ""} when value >= 1 -> value
      _ -> default
    end
  end
end
