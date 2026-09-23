defmodule BackendElixir.Flags.FeatureFlagPoller do
  @moduledoc """
  `feature_flags`テーブルを10秒間隔で直接ポーリングするGenServer
  (backend-c/backend-cpp/backend-java/backend-kotlin/backend-pythonのFeatureFlagPollerと同じ設計、
  bffのようなHTTPポーリングではなく自分専用のDB接続で直接ポーリングする)。

  【JwksCacheServerと同じ、このElixir実装の一貫した設計判断】このキャッシュもGenServerに
  排他的に所有させ、他プロセス(External.Handler)は`GenServer.call/2`でのみ問い合わせる
  (ロックという概念を使わない、backend-elixir/README.md「アーキテクチャ選定」節参照)。
  Ecto.Repoはmix.exsに既に依存として存在するため、生SQLを`Ecto.Adapters.SQL.query/3`で
  そのまま発行する(Ectoの通常のクエリDSLを介さない、独立したポーリング用の接続経路)
  """

  use GenServer

  alias BackendElixir.Repo

  @poll_interval_ms 10_000

  # ---- Client API ----

  def start_link(opts) do
    {name, opts} = Keyword.pop(opts, :name, __MODULE__)
    GenServer.start_link(__MODULE__, opts, name: name)
  end

  @doc "flag_keyのvariationを返す。enabled=falseまたは未登録の場合はdefault_valueを返す"
  def variation(server \\ __MODULE__, flag_key, default_value) do
    GenServer.call(server, {:variation, flag_key, default_value})
  end

  @doc "テスト専用: 10秒待たずに即座にポーリングし直す"
  def refresh(server \\ __MODULE__) do
    GenServer.call(server, :refresh, 10_000)
  end

  # ---- Server callbacks ----

  @impl true
  def init(_opts) do
    state = %{flags: poll_once()}
    schedule_poll()
    {:ok, state}
  end

  @impl true
  def handle_call({:variation, flag_key, default_value}, _from, state) do
    reply =
      case Map.get(state.flags, flag_key) do
        %{enabled: true, default_variation: variation} -> variation
        _ -> default_value
      end

    {:reply, reply, state}
  end

  @impl true
  def handle_call(:refresh, _from, state) do
    flags = poll_once()
    {:reply, :ok, %{state | flags: flags}}
  end

  @impl true
  def handle_info(:poll, state) do
    flags = poll_once()
    schedule_poll()
    {:noreply, %{state | flags: flags}}
  end

  defp schedule_poll do
    Process.send_after(self(), :poll, @poll_interval_ms)
  end

  defp poll_once do
    case Ecto.Adapters.SQL.query(Repo, "SELECT flag_key, enabled, default_variation FROM feature_flags", []) do
      {:ok, %{rows: rows}} ->
        Enum.reduce(rows, %{}, fn [flag_key, enabled, default_variation], acc ->
          # 【実機検証が必要】Ecto.Adapters.SQL.queryは生SQLのためEcto.Schemaの:boolean型変換を
          # 経由せず、MyXQLがTINYINT(1)をどう返すかに依存する(1/0の整数、またはtrue/falseの
          # どちらかがありうる)。両方を許容しておく
          Map.put(acc, flag_key, %{enabled: enabled == 1 or enabled == true, default_variation: default_variation})
        end)

      {:error, _reason} ->
        %{}
    end
  end
end
