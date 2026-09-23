defmodule BackendElixir.Repository.TaskSchema do
  @moduledoc """
  backend/migrations(golang-migrate)が正本のtasksテーブルへのEcto.Schemaマッピング。
  Ectoは型変換(パラメータ->型付き構造体)にのみ使い、ビジネスルール検証(名前の文字数制限等)は
  BackendElixir.Domain.Validationが担う(README.md「アーキテクチャ選定」節参照)
  """
  use Ecto.Schema

  @primary_key {:id, :id, autogenerate: true}
  schema "tasks" do
    field :name, :string
    field :description, :string
    field :status, :integer
    field :finished_on, :date
    field :user_id, :integer
    field :created_at, :naive_datetime
    field :updated_at, :naive_datetime
  end
end

defmodule BackendElixir.Repository.LabelSchema do
  @moduledoc false
  use Ecto.Schema

  @primary_key {:id, :id, autogenerate: true}
  schema "labels" do
    field :name, :string
    field :created_at, :naive_datetime
    field :updated_at, :naive_datetime
  end
end

defmodule BackendElixir.Repository.TaskLabelSchema do
  @moduledoc false
  use Ecto.Schema

  @primary_key {:id, :id, autogenerate: true}
  schema "task_labels" do
    field :task_id, :integer
    field :label_id, :integer
    field :created_at, :naive_datetime
    field :updated_at, :naive_datetime
  end
end

defmodule BackendElixir.Repository.UserSchema do
  @moduledoc false
  use Ecto.Schema

  @primary_key {:id, :id, autogenerate: true}
  schema "users" do
    field :email, :string
    field :name, :string
    field :role, :integer
    field :created_at, :naive_datetime
    field :updated_at, :naive_datetime
  end
end

defmodule BackendElixir.Repository.UserKeycloakSchema do
  @moduledoc false
  use Ecto.Schema

  @primary_key {:id, :id, autogenerate: true}
  schema "user_keycloaks" do
    field :user_id, :integer
    field :keycloak_sub, :string
    field :created_at, :naive_datetime
    field :updated_at, :naive_datetime
  end
end
