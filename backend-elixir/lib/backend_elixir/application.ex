defmodule BackendElixir.Application do
  @moduledoc """
  トップレベルのSupervisor(`:one_for_one`)。Ecto.Repo・JWKSキャッシュGenServer(local-rsa/
  keycloakの2issuer分)・REST(Plug+Cowboy)・gRPCサーバーを配下に置く。
  いずれかの子プロセスが異常終了しても、Supervisorが再起動する(let it crash、
  README.md「アーキテクチャ選定」節参照)。DBコネクションプール(Ecto.Repo)自体も
  Ectoが内部でSupervisorとして提供しており、この配下に含めるだけでよい
  """

  use Application
  require Logger

  alias BackendElixir.Auth.{Dispatcher, HmacVerifier, JwksCacheServer, JwksVerifier}
  alias BackendElixir.Config
  alias BackendElixir.Flags.FeatureFlagPoller

  @local_rsa_cache BackendElixir.Auth.JwksCacheServer.LocalRsa
  @keycloak_cache BackendElixir.Auth.JwksCacheServer.Keycloak

  @impl true
  def start(_type, _args) do
    config = Config.from_env()
    configure_log_level(config.log_level)

    Logger.info(fn ->
      "backend-elixir starting: HTTP_ADDR=:#{config.http_addr} GRPC_ADDR=:#{config.grpc_addr} " <>
        "EXTERNAL_HTTP_ADDR=:#{config.external_http_addr} db=#{config.db_host}:#{config.db_port}/#{config.db_schema}"
    end)

    Application.put_env(:backend_elixir, :dispatcher, build_dispatcher(config))
    Application.put_env(:backend_elixir, :external_api_client_id, config.external_api_client_id)

    base_children = [
      {BackendElixir.Repo,
       hostname: config.db_host,
       port: config.db_port,
       username: config.db_user,
       password: config.db_password,
       database: config.db_schema,
       pool_size: 10},
      Supervisor.child_spec({JwksCacheServer, name: @local_rsa_cache, jwks_url: config.local_rsa_jwks_url},
        id: @local_rsa_cache
      ),
      Supervisor.child_spec({JwksCacheServer, name: @keycloak_cache, jwks_url: config.keycloak_jwks_url},
        id: @keycloak_cache
      ),
      FeatureFlagPoller
    ]

    # 【テスト時はREST/gRPC/外部APIリスナーを起動しない】mix testはこのApplicationモジュール自体を
    # 起動するが、単体テスト(test/unit)はモジュールを直接呼ぶだけでHTTP/gRPCサーバーは
    # 不要であり、結合テスト(test/integration)は各テストファイル自身が明示的にテスト用の
    # ポートでサーバーを起動する(backend-java/backend-kotlin/backend-pythonのテスト構成と
    # 同じ「本番のmain()とは別に、テストが必要な分だけ明示的に起動する」設計)。
    # 常時起動すると、複数のテスト実行やCI環境で固定ポートの衝突を招く
    children =
      if Mix.env() == :test do
        base_children
      else
        base_children ++
          [
            {Plug.Cowboy,
             scheme: :http, plug: BackendElixir.Rest.Router, options: [port: String.to_integer(config.http_addr)]},
            {GRPC.Server.Supervisor,
             endpoint: BackendElixir.Grpc.Endpoint, port: String.to_integer(config.grpc_addr), start_server: true},
            Supervisor.child_spec(
              {Plug.Cowboy,
               scheme: :http,
               plug: BackendElixir.External.Handler,
               options: [port: String.to_integer(config.external_http_addr)]},
              id: BackendElixir.External.Handler
            )
          ]
      end

    opts = [strategy: :one_for_one, name: BackendElixir.Supervisor]

    case Supervisor.start_link(children, opts) do
      {:ok, pid} ->
        Logger.info(fn -> "backend-elixir listening: REST=:#{config.http_addr} GRPC=:#{config.grpc_addr}" end)
        {:ok, pid}

      error ->
        error
    end
  end

  # 【LOG_LEVEL対応】backend(Go)/bff/gateway/goと同じ"debug"/"info"/"warn"/"error"の4値を
  # 受け付ける。Logger.configure/1はランタイムのログレベルを変更する(config.exsに
  # compile_time_purge_matching等の設定は無いため、debugログ自体はコンパイル時に
  # 削除されておらず、再ビルド無しで有効・無効を切り替えられる)
  defp configure_log_level(level_str) do
    level =
      case level_str do
        "debug" -> :debug
        "info" -> :info
        "warn" -> :warning
        "error" -> :error
        _ -> :info
      end

    Logger.configure(level: level)
  end

  defp build_dispatcher(config) do
    Dispatcher.new()
    |> Dispatcher.register(
      Dispatcher.local_hmac_issuer(),
      HmacVerifier,
      HmacVerifier.new(config.local_hmac_secret, Dispatcher.local_hmac_issuer(), config.expected_audience)
    )
    |> Dispatcher.register(
      Dispatcher.local_rsa_issuer(),
      JwksVerifier,
      JwksVerifier.new(@local_rsa_cache, Dispatcher.local_rsa_issuer(), config.expected_audience)
    )
    |> Dispatcher.register(
      config.keycloak_issuer,
      JwksVerifier,
      JwksVerifier.new(@keycloak_cache, config.keycloak_issuer, config.expected_audience)
    )
  end
end
