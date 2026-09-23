defmodule BackendElixir.Auth.JwksCacheServer do
  @moduledoc """
  kid(鍵ID)ごとの公開鍵キャッシュを保有するGenServer。

  【このElixir実装で最も学習価値の高い設計判断の1つ】backend-kotlinは`Mutex`(kotlinx.coroutines.sync)で
  保護した共有Mapとしてこのキャッシュを実装し(backend-kotlin/src/main/kotlin/com/bffgin/backend/
  auth/JwksVerifier.kt参照)、backend-javaは`ConcurrentHashMap`(java.util.concurrent)を使った
  (backend-java/src/main/java/com/bffgin/backend/auth/JwksVerifier.java参照)。どちらも
  「共有可変状態をロック/並行対応コレクションで守る」という設計である。

  このElixir実装は対照的に、**ロックという概念そのものを使わない**。GenServerプロセス自身が
  kid->公開鍵のマップを排他的に所有し、他のプロセス(HTTPハンドラ・gRPCハンドラ)は
  `GenServer.call/2`によるメッセージ送信でのみこの状態にアクセスする。BEAMのプロセスは
  同時に1つのメッセージしか処理しないため、複数のリクエストが同時にキャッシュへアクセス
  しようとしても、GenServerのメッセージキューが自然に直列化してくれる。「メモリを共有して
  ロックで守る」のではなく「状態をプロセスに閉じ込め、メッセージでやり取りする」という、
  アクターモデルの本質的な設計思想の実演になっている(README.md「アーキテクチャ選定」節参照)。

  【let it crashの実演】JWKSレスポンスの構文解析に失敗するなど、通常運用では起こり得ない
  明確に異常な状況が起きた場合、このGenServerは防御的にエラーを握りつぶさず、そのままクラッシュ
  する(`refresh!/1`のfetch_and_parse!が例外を投げっぱなしにする)。上位のSupervisorがこれを検知し、
  空のキャッシュから綺麗に再起動する。「未知のkidを検証できず拒否する」という通常の(正常系の)
  制御フローとは区別している点に注意(それは`handle_call({:lookup, kid}, ...)`が単に`nil`を
  返すだけの、クラッシュではない通常のエラーハンドリング)
  """

  use GenServer
  require Logger

  # ---- Client API ----

  def start_link(opts) do
    {name, opts} = Keyword.pop(opts, :name)

    if name do
      GenServer.start_link(__MODULE__, opts, name: name)
    else
      GenServer.start_link(__MODULE__, opts)
    end
  end

  @doc "kidに対応する鍵(Joken.Signer)を返す。無ければnil"
  def lookup(server \\ __MODULE__, kid) do
    GenServer.call(server, {:lookup, kid})
  end

  @doc "JWKSエンドポイントへ再取得を行い、キャッシュを丸ごと入れ替える"
  def refresh(server \\ __MODULE__) do
    GenServer.call(server, :refresh, 10_000)
  end

  @doc "テスト専用: 意図的にクラッシュさせるための異常系トリガー(let it crashの実演用)"
  def crash_with_malformed_response(server \\ __MODULE__, malformed_body) do
    GenServer.call(server, {:crash_with_malformed_response, malformed_body})
  end

  # ---- Server callbacks ----

  @impl true
  def init(opts) do
    jwks_url = Keyword.fetch!(opts, :jwks_url)
    http_client = Keyword.get(opts, :http_client, BackendElixir.Auth.JwksHttpClient)
    {:ok, %{jwks_url: jwks_url, http_client: http_client, keys: %{}}}
  end

  @impl true
  def handle_call({:lookup, kid}, _from, state) do
    {:reply, Map.get(state.keys, kid), state}
  end

  @impl true
  def handle_call(:refresh, _from, state) do
    Logger.debug(fn -> "jwks_cache refresh start jwks_url=#{state.jwks_url}" end)

    case fetch_and_parse(state.http_client, state.jwks_url) do
      {:ok, keys} ->
        Logger.debug(fn -> "jwks_cache refresh ok kid_count=#{map_size(keys)}" end)
        {:reply, :ok, %{state | keys: keys}}

      {:error, reason} ->
        Logger.debug(fn -> "jwks_cache refresh failed reason=#{inspect(reason)}" end)
        {:reply, {:error, reason}, state}
    end
  end

  @impl true
  def handle_call({:crash_with_malformed_response, malformed_body}, _from, state) do
    # 【let it crashの実演】正常な再取得(handle_call(:refresh, ...))はHTTP失敗やJSON構文エラーを
    # {:error, reason}として呼び出し元へ返す「通常のエラー処理」だが、ここでは意図的に
    # `parse_keys!/1`(末尾に!が付く、失敗時に例外を投げる版)を使い、プロセスをクラッシュさせる。
    # 供給元のSupervisorがこのクラッシュを検知し、空のキャッシュ状態からこのGenServerを
    # 再起動する(テストで実際に再起動を確認する、test/unit/jwks_cache_server_test.exs参照)
    keys = parse_keys!(malformed_body)
    {:reply, :ok, %{state | keys: keys}}
  end

  defp fetch_and_parse(http_client, jwks_url) do
    with {:ok, body} <- http_client.fetch(jwks_url) do
      {:ok, parse_keys(body)}
    end
  end

  defp parse_keys(body) do
    body
    |> Map.get("keys", [])
    |> Enum.reduce(%{}, fn jwk, acc ->
      case build_signer(jwk) do
        {:ok, kid, signer} -> Map.put(acc, kid, signer)
        # 【バッド/グッドプラクティス、backend-java/backend-kotlin/backend-pythonと同じ設計】
        # 特定の鍵1件の構築に失敗しただけでJWKS取得全体を失敗させると、他の正常な鍵まで
        # 使えなくなってしまう。ここでは該当エントリだけスキップし、他の鍵は正常に反映する
        :skip -> acc
      end
    end)
  end

  # crash_with_malformed_response専用: parse_keysと違い異常系で例外を投げっぱなしにする
  defp parse_keys!(body) do
    keys = Map.fetch!(body, "keys")

    Enum.reduce(keys, %{}, fn jwk, acc ->
      {:ok, kid, signer} = build_signer(jwk)
      Map.put(acc, kid, signer)
    end)
  end

  defp build_signer(%{"kty" => "RSA", "kid" => kid, "n" => _n, "e" => _e} = jwk) when is_binary(kid) do
    use_field = Map.get(jwk, "use", "")

    if use_field == "" or use_field == "sig" do
      try do
        signer = Joken.Signer.create("RS256", jwk)
        {:ok, kid, signer}
      rescue
        _ -> :skip
      end
    else
      :skip
    end
  end

  defp build_signer(_jwk), do: :skip
end
