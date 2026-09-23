defmodule BackendElixir.Repository.TaskRepository do
  @moduledoc """
  Ecto(標準的作法として採用、README.md「アーキテクチャ選定」節参照)。
  ビジネスルールの検証はDomain.Validationが担い、ここはDBアクセスに専念する。

  【このElixir実装の並行処理モデルにおける立ち位置】backend-java(Virtual Threads)は
  「並行処理の安全性を自動化する」設計、backend-kotlin(Dispatchers.IO)は「型システムと
  明示的なディスパッチャ選択で保証する」設計、backend-python(aiomysql)は「ドライバ自体が
  非同期ネイティブなので隔離が不要」という設計だった。Elixir/Ectoの`MyXQL`アダプタは
  DBコネクションごとに専用のプロセスを持ち、呼び出し元プロセスは`GenServer.call`相当の
  メッセージ送信でDB操作を依頼する。BEAMは呼び出し元プロセスがDB応答を待っている間、
  そのプロセスをスケジューラから外し他のプロセスへCPUを回せる(プロセス自体が軽量なため、
  「1プロセスが1つのDB待ちで詰まる」ことがシステム全体のスループットに与える影響が
  スレッドベースの言語よりも小さい)。「非同期/同期」という区分そのものがBEAMでは
  あまり意味を持たない、という第4の視点になる(README.md「アーキテクチャ選定」節参照)
  """

  import Ecto.Query

  alias BackendElixir.Domain.{Label, Task, TaskStatus, User}
  alias BackendElixir.Repository.{LabelSchema, TaskLabelSchema, TaskSchema, UserKeycloakSchema, UserSchema}
  alias BackendElixir.Repo

  @doc "v1(REST)向け: offsetページング"
  def list_offset(user_id, limit, offset) do
    total =
      TaskSchema
      |> where([t], t.user_id == ^user_id)
      |> select([t], count(t.id))
      |> Repo.one()

    # 【他言語と同じtie-break、backend-c/backend-cppで見つかった既知バグの再発防止】
    # created_at同値(DATETIME列は秒精度)だけでは並び順が不定になるため、idを追加のtie-breakとする
    tasks =
      TaskSchema
      |> where([t], t.user_id == ^user_id)
      |> order_by([t], desc: t.created_at, desc: t.id)
      |> limit(^limit)
      |> offset(^offset)
      |> Repo.all()
      |> attach_labels()
      |> Enum.map(&to_domain_task/1)

    {:ok, tasks, total}
  end

  @doc """
  v2(gRPC)向け: id昇順のkeyset(cursor)ページング。
  cursorの実体はuint64(直前ページ最後のtask.id、0=先頭)、合成キーは使わない(CONTRACT.mdセクション5)
  """
  def list_cursor(user_id, after_id, limit) do
    query =
      TaskSchema
      |> where([t], t.user_id == ^user_id)
      |> order_by([t], asc: t.id)
      |> limit(^limit)

    query = if after_id > 0, do: where(query, [t], t.id > ^after_id), else: query

    tasks =
      query
      |> Repo.all()
      |> attach_labels()
      |> Enum.map(&to_domain_task/1)

    {:ok, tasks}
  end

  @doc """
  他ユーザーのtaskは見えない(所有権分離)。
  【実機検証で見つけた実バグ】このメソッドの戻り値だけは`{:error, TaskError.not_found()}`と
  TaskError構造体で包んで返す(find_user_by_id/find_user_by_keycloak_subの`{:error, :not_found}`
  という裸のatomとは意図的に異なる)。理由: 前者2つはuser_resolver.exという内部レイヤーだけが
  消費し、そこで`{:error, :not_found} -> {:error, TaskError.user_not_provisioned()}`へ
  明示的に変換している。一方find_by_id/2はREST(Router)・gRPC(TaskService)の各ハンドラが
  直接`with`チェーンの終端として使い、そのままErrorMapper/logged関数へ渡すため、
  最初からTaskError型で統一しておく必要がある(このズレが原因でGetTask実装時に
  CaseClauseError(gRPC側)/FunctionClauseError(REST側)を実際に引き起こした、
  README.md「実装時に判明した既知の差異」参照)
  """
  def find_by_id(task_id, user_id) do
    case TaskSchema |> where([t], t.id == ^task_id and t.user_id == ^user_id) |> Repo.one() do
      nil -> {:error, BackendElixir.Domain.TaskError.not_found()}
      row -> {:ok, row |> attach_labels_one() |> to_domain_task()}
    end
  end

  def create(user_id, input, status_db) do
    now = utc_now_naive()

    Ecto.Multi.new()
    |> Ecto.Multi.insert(:task, %TaskSchema{
      name: input.name,
      description: input.description,
      status: status_db,
      finished_on: input.finished_on,
      user_id: user_id,
      created_at: now,
      updated_at: now
    })
    |> Ecto.Multi.run(:labels, fn _repo, %{task: task} -> insert_labels(task.id, input.label_ids, now) end)
    |> Repo.transaction()
    |> case do
      {:ok, %{task: task}} -> {:ok, task.id}
      {:error, _step, reason, _changes} -> {:error, reason}
    end
  end

  @doc "戻り値: {:ok, true}更新できた, {:ok, false}対象行が(他人のtaskも含め)見つからない"
  def update(task_id, user_id, input, status_db) do
    now = utc_now_naive()

    Ecto.Multi.new()
    |> Ecto.Multi.run(:updated_count, fn repo, _changes ->
      {count, _} =
        repo.update_all(
          from(t in TaskSchema, where: t.id == ^task_id and t.user_id == ^user_id),
          set: [name: input.name, description: input.description, status: status_db, finished_on: input.finished_on, updated_at: now]
        )

      {:ok, count}
    end)
    |> Ecto.Multi.run(:labels, fn _repo, %{updated_count: count} ->
      if count > 0, do: insert_labels(task_id, input.label_ids, now), else: {:ok, :skipped}
    end)
    |> Repo.transaction()
    |> case do
      {:ok, %{updated_count: 0}} -> {:ok, false}
      {:ok, %{updated_count: _}} -> {:ok, true}
      {:error, _step, reason, _changes} -> {:error, reason}
    end
  end

  @doc """
  tasksとtask_labelsの削除を1つのトランザクションで包む。
  task_labelsには外部キー制約が無い(migrations/000004)ため、トランザクション無しで個別に
  DELETEすると、両文の間でプロセスが落ちた場合にtask_labelsの孤立行が残り得る
  (backend-rust/backend-c/backend-cpp/backend-java/backend-kotlin/backend-pythonと同じ設計。
  backend-rustにはかつてこの保護が欠けている既知バグがあり、後に修正された経緯がある)
  """
  def delete(task_id, user_id) do
    Ecto.Multi.new()
    |> Ecto.Multi.run(:deleted_count, fn repo, _changes ->
      {count, _} = repo.delete_all(from(t in TaskSchema, where: t.id == ^task_id and t.user_id == ^user_id))
      {:ok, count}
    end)
    |> Ecto.Multi.run(:labels, fn repo, %{deleted_count: count} ->
      if count > 0 do
        repo.delete_all(from(tl in TaskLabelSchema, where: tl.task_id == ^task_id))
        {:ok, :deleted}
      else
        {:ok, :skipped}
      end
    end)
    |> Repo.transaction()
    |> case do
      {:ok, %{deleted_count: 0}} -> {:ok, false}
      {:ok, %{deleted_count: _}} -> {:ok, true}
    end
  end

  @doc "JWT認証のuser_id解決用。ローカル発行issuerのsubはusers.idそのもの、存在確認のみ行う"
  def find_user_by_id(user_id) do
    case Repo.get(UserSchema, user_id) do
      nil -> {:error, :not_found}
      row -> {:ok, %User{id: row.id, email: row.email, name: row.name}}
    end
  end

  @doc """
  Keycloak発行issuerのsub=keycloak_subは、usersテーブルには無くuser_keycloaksテーブルに
  分離されている(migration 000008_split_user_credentials、CONTRACT.mdセクション16.2)ため
  JOIN経由で引く
  """
  def find_user_by_keycloak_sub(keycloak_sub) do
    query =
      from u in UserSchema,
        join: uk in UserKeycloakSchema,
        on: uk.user_id == u.id,
        where: uk.keycloak_sub == ^keycloak_sub,
        select: u

    case Repo.one(query) do
      nil -> {:error, :not_found}
      row -> {:ok, %User{id: row.id, email: row.email, name: row.name}}
    end
  end

  defp insert_labels(task_id, label_ids, now) do
    Repo.delete_all(from(tl in TaskLabelSchema, where: tl.task_id == ^task_id))

    # 【他言語で見つかった既知バグと同種】label_idsに同じidが重複して含まれる場合(例: [3,3,5])、
    # 重複除去せずそのままINSERTすると2回目の(task_id,3)でtask_labelsの(task_id,label_id)への
    # UNIQUE制約(migrations/000004)に違反する(backend(Go)・backend-rust・backend-java・
    # backend-kotlin・backend-pythonで見つかった同根のバグ)。Enum.uniqは最初の出現順を保つ
    deduped = Enum.uniq(label_ids)

    if deduped != [] do
      rows =
        Enum.map(deduped, fn label_id ->
          %{task_id: task_id, label_id: label_id, created_at: now, updated_at: now}
        end)

      Repo.insert_all(TaskLabelSchema, rows)
    end

    {:ok, :inserted}
  end

  defp attach_labels(tasks) when is_list(tasks) do
    if tasks == [] do
      []
    else
      ids = Enum.map(tasks, & &1.id)

      labels_by_task =
        from(tl in TaskLabelSchema,
          join: l in LabelSchema,
          on: l.id == tl.label_id,
          where: tl.task_id in ^ids,
          select: {tl.task_id, %Label{id: l.id, name: l.name}}
        )
        |> Repo.all()
        |> Enum.group_by(fn {task_id, _label} -> task_id end, fn {_task_id, label} -> label end)

      Enum.map(tasks, fn t -> {t, Map.get(labels_by_task, t.id, [])} end)
    end
  end

  defp attach_labels_one(task) do
    [{task, labels}] = attach_labels([task])
    {task, labels}
  end

  defp to_domain_task({row, labels}) do
    {status_db, status_wire} =
      case TaskStatus.from_db_value(row.status) do
        {:ok, db, wire} -> {db, wire}
        :error -> {1, "waiting"}
      end

    %Task{
      id: row.id,
      name: row.name,
      description: row.description,
      status_db: status_db,
      status_wire: status_wire,
      finished_on: row.finished_on,
      user_id: row.user_id,
      labels: labels,
      created_at: row.created_at,
      updated_at: row.updated_at
    }
  end

  # 【実機検証で確認する必要がある点、README.md「実装時に判明した既知の差異」参照】
  # MyXQL/EctoがDATETIME列をどう返すか(タイムゾーン変換の有無)は、backend-java(JDBCで
  # システムデフォルトタイムゾーン経由の変換バグが実際に見つかった)・backend-kotlin/
  # backend-python(ドライバがナイーブな値をそのまま返すため変換バグが無い)と同じ観点で
  # 実機検証が必要。utc_now_naive/0はUTCの壁時計値をナイーブな(タイムゾーン情報を持たない)
  # NaiveDateTimeとして書き込む(MySQLのDATETIME列自体がタイムゾーン情報を持たないため)
  defp utc_now_naive do
    DateTime.utc_now() |> DateTime.to_naive() |> NaiveDateTime.truncate(:second)
  end
end
