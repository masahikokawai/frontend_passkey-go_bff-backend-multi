defmodule BackendElixir.Rest.TaskJson do
  @moduledoc """
  CONTRACT.mdセクション5.1のJSON形状(スネークケース)。
  【backend(Go)の実際の挙動に合わせた既知の差異】taskDTOToJSON(backend/internal/handler/v1/task.go)は
  user_idをレスポンスに含めていない(CONTRACT.md本文の例には書かれているが、実装はそうなっていない。
  ワイヤー契約パリティの原則(セクション20.5)に従い、ドキュメントではなく実際の挙動に合わせる。
  backend-java/backend-kotlin/backend-python/backend-rust/backend-c/backend-cppの全てで
  同じ既知の差異が確認・踏襲されている)
  """

  def to_json(task) do
    %{
      id: task.id,
      name: task.name,
      description: task.description,
      status: task.status_wire,
      finished_on: Date.to_iso8601(task.finished_on),
      labels: Enum.map(task.labels, fn label -> %{id: label.id, name: label.name} end),
      created_at: to_rfc3339(task.created_at),
      updated_at: to_rfc3339(task.updated_at)
    }
  end

  defp to_rfc3339(%NaiveDateTime{} = dt) do
    NaiveDateTime.to_iso8601(dt) <> "+00:00"
  end
end
