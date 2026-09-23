defmodule BackendElixir.Domain.TaskStatus do
  @moduledoc "backend(Go)のTaskStatus(waiting=1, work_in_progress=2, completed=3)と同じマッピング"

  @mapping [
    {1, "waiting"},
    {2, "work_in_progress"},
    {3, "completed"}
  ]

  def from_wire_value(value) do
    Enum.find_value(@mapping, fn {db, wire} -> if wire == value, do: {:ok, db, wire} end) ||
      :error
  end

  def from_db_value(value) do
    Enum.find_value(@mapping, fn {db, wire} -> if db == value, do: {:ok, db, wire} end) ||
      :error
  end
end

defmodule BackendElixir.Domain.Label do
  @moduledoc false
  defstruct [:id, :name]
end

defmodule BackendElixir.Domain.User do
  @moduledoc false
  defstruct [:id, :email, :name]
end

defmodule BackendElixir.Domain.TaskInput do
  @moduledoc false
  defstruct [:name, :description, :status_raw, :finished_on, label_ids: []]
end

defmodule BackendElixir.Domain.Task do
  @moduledoc """
  user_idはワイヤーに乗せない(REST/gRPCとも)。backend(Go)実装がtaskDTOToJSONでuser_idを
  含めていない実際の挙動に合わせている(CONTRACT.mdセクション5.1本文の例には書かれているが、
  ワイヤー契約パリティの原則(セクション20.5)により、ドキュメントではなく実際の挙動に合わせる。
  backend-java/backend-kotlin/backend-python/backend-rust/backend-c/backend-cppの全てで
  同じ既知の差異が確認・踏襲されている)
  """
  defstruct [:id, :name, :description, :status_db, :status_wire, :finished_on, :user_id, :labels, :created_at, :updated_at]
end
