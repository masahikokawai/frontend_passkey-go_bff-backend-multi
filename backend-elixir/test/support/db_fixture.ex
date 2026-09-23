defmodule BackendElixir.TestSupport.DbFixture do
  @moduledoc """
  実DB(docker-compose上のMySQL)結合テスト共通のfixture。テストごとに一意なemail/keycloak_subで
  ユーザー・ラベルを作り、共有の開発用DBを汚さない(backend-java/backend-kotlin/backend-python/
  backend-c/backend-cppのDbTestFixtureと同じ設計。過去にbackend-rustで固定fixture行の共有が
  原因のテスト間干渉が見つかった経緯があるため、必ずテストごとに一意な行を作る)
  """

  alias BackendElixir.Repo
  alias BackendElixir.Repository.{LabelSchema, UserKeycloakSchema, UserSchema}

  # 【MySQLの制約】MySQLのINSERTはRETURNING句をサポートしないため(PostgreSQLと異なる)、
  # Repo.insert_all/3にreturning:オプションは使えない。代わりにEcto.Schemaベースの
  # Repo.insert/1(内部でMySQLのOKパケットが返すlast_insert_idを使ってid自動採番の値を
  # 取得する)を使う
  def unique_suffix do
    "#{System.system_time(:microsecond)}-#{:crypto.strong_rand_bytes(4) |> Base.encode16(case: :lower)}"
  end

  def create_user(suffix) do
    now = now_naive()

    {:ok, user} =
      Repo.insert(%UserSchema{
        email: "backend-elixir-test-#{suffix}@example.com",
        name: "backend-elixir-test-#{suffix}",
        role: 1,
        created_at: now,
        updated_at: now
      })

    user.id
  end

  def create_user_with_keycloak_sub(suffix, keycloak_sub) do
    user_id = create_user(suffix)
    now = now_naive()

    Repo.insert(%UserKeycloakSchema{user_id: user_id, keycloak_sub: keycloak_sub, created_at: now, updated_at: now})

    user_id
  end

  def create_label(suffix) do
    now = now_naive()
    {:ok, label} = Repo.insert(%LabelSchema{name: "backend-elixir-test-label-#{suffix}", created_at: now, updated_at: now})
    label.id
  end

  @doc "ベストエフォートの後始末(backend-java/backend-kotlin/backend-pythonと同じ方針)"
  def cleanup_user(user_id) do
    import Ecto.Query

    Repo.delete_all(from(tl in "task_labels", where: tl.task_id in subquery(from(t in "tasks", where: t.user_id == ^user_id, select: t.id))))
    Repo.delete_all(from(t in "tasks", where: t.user_id == ^user_id))
    Repo.delete_all(from(uk in "user_keycloaks", where: uk.user_id == ^user_id))
    Repo.delete_all(from(u in "users", where: u.id == ^user_id))
  rescue
    _ -> :ok
  end

  def cleanup_label(label_id) do
    import Ecto.Query
    Repo.delete_all(from(l in "labels", where: l.id == ^label_id))
  rescue
    _ -> :ok
  end

  def count_task_labels(task_id) do
    import Ecto.Query
    Repo.aggregate(from(tl in "task_labels", where: tl.task_id == ^task_id), :count)
  end

  defp now_naive do
    DateTime.utc_now() |> DateTime.to_naive() |> NaiveDateTime.truncate(:second)
  end
end
