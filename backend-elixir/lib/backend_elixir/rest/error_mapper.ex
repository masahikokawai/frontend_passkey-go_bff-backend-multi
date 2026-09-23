defmodule BackendElixir.Rest.ErrorMapper do
  @moduledoc """
  backend-java/backend-kotlin/backend-python/backend-rustのRestErrorMapper/status_and_bodyと
  1文字も変えていないJSON形状・HTTPステータス(CONTRACT.mdセクション20.5のワイヤー契約パリティ)
  """

  alias BackendElixir.Domain.TaskError

  def status_and_body(%TaskError{kind: :validation, message: message}) do
    {422, %{error: "validation_error", message: message}}
  end

  def status_and_body(%TaskError{kind: kind}) do
    {status, error_key} = kind_to_status_and_error(kind)
    {status, %{error: error_key}}
  end

  defp kind_to_status_and_error(:unauthorized), do: {401, "unauthorized"}
  defp kind_to_status_and_error(:unauthenticated), do: {401, "unauthenticated"}
  defp kind_to_status_and_error(:user_not_provisioned), do: {403, "user_not_provisioned"}
  defp kind_to_status_and_error(:invalid_request), do: {400, "invalid_request"}
  defp kind_to_status_and_error(:invalid_id), do: {400, "invalid_id"}
  defp kind_to_status_and_error(:user_id_required), do: {400, "user_id_required"}
  defp kind_to_status_and_error(:invalid_user_id), do: {400, "invalid_user_id"}
  defp kind_to_status_and_error(:invalid_status), do: {422, "invalid_status"}
  defp kind_to_status_and_error(:invalid_finished_on), do: {422, "invalid_finished_on"}
  defp kind_to_status_and_error(:not_found), do: {404, "not_found"}
  defp kind_to_status_and_error(:internal), do: {500, "internal_server_error"}
end
