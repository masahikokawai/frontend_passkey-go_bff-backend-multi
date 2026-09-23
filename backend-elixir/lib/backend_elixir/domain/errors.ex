defmodule BackendElixir.Domain.TaskError do
  @moduledoc """
  backend-java/backend-kotlin/backend-python/backend-rustのTaskError/RestErrorと同じ分類。
  JSON形状・HTTPステータス・gRPCステータスへの変換はトランスポート層(rest/grpc)がそれぞれ担う。
  Elixirには例外(exception)機構もあるが、ここでは`{:error, %TaskError{}}`というタプルで
  呼び出し元に返す設計にする(GenServerの`{:reply, ...}`や、他の値と同じくパターンマッチで
  分岐できるようにするため。JavaException/Python例外のように呼び出し元を強制的に巻き戻す
  必要はこの用途には無い)
  """

  defstruct [:kind, :message]

  @type kind ::
          :unauthorized
          | :unauthenticated
          | :user_not_provisioned
          | :invalid_request
          | :invalid_id
          | :user_id_required
          | :invalid_user_id
          | :invalid_status
          | :invalid_finished_on
          | :validation
          | :not_found
          | :internal

  def unauthorized, do: %__MODULE__{kind: :unauthorized, message: "unauthorized"}

  # 【内部REST/gRPCの:unauthorizedとは意図的に別のkind】外部公開API(External.Handler)の
  # 認証失敗はCONTRACT.mdセクション11の規約により"unauthenticated"という別の文字列を返す
  # (backend-c/backend-cpp/backend-java/backend-kotlin/backend-python/backend-rustの
  # 外部公開APIハンドラと同じ区別)
  def unauthenticated, do: %__MODULE__{kind: :unauthenticated, message: "unauthenticated"}
  def user_id_required, do: %__MODULE__{kind: :user_id_required, message: "user_id_required"}
  def invalid_user_id, do: %__MODULE__{kind: :invalid_user_id, message: "invalid_user_id"}
  def user_not_provisioned, do: %__MODULE__{kind: :user_not_provisioned, message: "user_not_provisioned"}
  def invalid_request, do: %__MODULE__{kind: :invalid_request, message: "invalid_request"}
  def invalid_id, do: %__MODULE__{kind: :invalid_id, message: "invalid_id"}
  def invalid_status, do: %__MODULE__{kind: :invalid_status, message: "invalid_status"}
  def invalid_finished_on, do: %__MODULE__{kind: :invalid_finished_on, message: "invalid_finished_on"}
  def validation(message), do: %__MODULE__{kind: :validation, message: message}
  def not_found, do: %__MODULE__{kind: :not_found, message: "not_found"}
  def internal(message), do: %__MODULE__{kind: :internal, message: message}
end
