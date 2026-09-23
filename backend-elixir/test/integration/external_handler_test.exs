defmodule BackendElixir.External.HandlerTest do
  @moduledoc """
  実DB+実HTTPリスナー(テスト専用ポート)を使った外部公開APIの結合テスト。
  モックJWKSサーバー(Keycloak相当)で実際に署名したRS256トークンを使う
  (backend-c/backend-cpp/backend-java/backend-kotlin/backend-pythonの外部API結合テストと同じ設計)
  """
  use ExUnit.Case, async: false
  @moduletag :integration

  alias BackendElixir.Auth.{Dispatcher, HmacVerifier, JwksCacheServer, JwksVerifier}
  alias BackendElixir.Domain.TaskInput
  alias BackendElixir.Repo
  alias BackendElixir.Repository.TaskRepository
  alias BackendElixir.TestSupport.{DbFixture, MockJwksServer, TestTokenHelper}

  @test_port 18118
  @keycloak_issuer "http://localhost:8082/realms/training"
  @audience "backend"
  @external_api_client_id "external-api-client"
  @hmac_secret "test-external-hmac-secret-integration"
  @flag_key "backend.external-tasks-pagination-v2"

  setup_all do
    {:ok, server_info} = MockJwksServer.start()
    MockJwksServer.reset_keys()
    private_key = TestTokenHelper.generate_rsa_key_pair()
    MockJwksServer.add_key("kid-ext", TestTokenHelper.public_jwk_fields(private_key))

    {:ok, cache_pid} = JwksCacheServer.start_link(jwks_url: server_info.jwks_url)

    dispatcher =
      Dispatcher.new()
      |> Dispatcher.register(
        Dispatcher.local_hmac_issuer(),
        HmacVerifier,
        HmacVerifier.new(@hmac_secret, Dispatcher.local_hmac_issuer(), @audience)
      )
      |> Dispatcher.register(@keycloak_issuer, JwksVerifier, JwksVerifier.new(cache_pid, @keycloak_issuer, @audience))

    Application.put_env(:backend_elixir, :dispatcher, dispatcher)
    Application.put_env(:backend_elixir, :external_api_client_id, @external_api_client_id)

    # 【重要】FeatureFlagPollerはApplication.start/2が既にデフォルト名(BackendElixir.Flags.
    # FeatureFlagPoller)で常時起動している(JwksCacheServerと違い、External.Handlerもこの
    # デフォルト名を素朴に参照するため、テストからも同じグローバルなインスタンスをそのまま使う。
    # start_supervised!で新規に起動しようとすると同名プロセスの:already_startedで失敗する
    start_supervised!(
      {Plug.Cowboy, scheme: :http, plug: BackendElixir.External.Handler, options: [port: @test_port]}
    )

    on_exit(fn -> MockJwksServer.stop(server_info) end)

    %{private_key: private_key}
  end

  setup do
    suffix = DbFixture.unique_suffix()
    user_id = DbFixture.create_user(suffix)
    on_exit(fn -> DbFixture.cleanup_user(user_id) end)
    %{user_id: user_id}
  end

  defp valid_token(private_key) do
    TestTokenHelper.make_rsa_token_with_azp(private_key, "kid-ext", @keycloak_issuer, @audience, "1", @external_api_client_id, 3600)
  end

  defp get(path, token \\ nil) do
    headers = if token, do: [{"authorization", "Bearer " <> token}], else: []
    Req.get!("http://127.0.0.1:#{@test_port}#{path}", headers: headers)
  end

  defp input(overrides) do
    base = %TaskInput{name: "ext-task", description: "d", status_raw: "waiting", finished_on: ~D[2099-01-01], label_ids: []}
    struct(base, overrides)
  end

  test "rejects missing authorization", %{user_id: user_id} do
    resp = get("/external/v1/tasks?user_id=#{user_id}")
    assert resp.status == 401
    assert resp.body["error"] == "unauthenticated"
  end

  test "rejects wrong azp", %{user_id: user_id, private_key: private_key} do
    token = TestTokenHelper.make_rsa_token_with_azp(private_key, "kid-ext", @keycloak_issuer, @audience, "1", "wrong-client", 3600)
    resp = get("/external/v1/tasks?user_id=#{user_id}", token)
    assert resp.status == 401
    assert resp.body["error"] == "unauthenticated"
  end

  test "rejects a valid local HMAC token even with correct azp semantics", %{user_id: user_id} do
    token = TestTokenHelper.make_hmac_token(@hmac_secret, Dispatcher.local_hmac_issuer(), @audience, to_string(user_id), 3600)
    resp = get("/external/v1/tasks?user_id=#{user_id}", token)
    assert resp.status == 401
    assert resp.body["error"] == "unauthenticated"
  end

  test "rejects missing user_id", %{private_key: private_key} do
    resp = get("/external/v1/tasks", valid_token(private_key))
    assert resp.status == 400
    assert resp.body["error"] == "user_id_required"
  end

  test "offset pagination works across page boundaries", %{user_id: user_id, private_key: private_key} do
    token = valid_token(private_key)
    {:ok, id1} = TaskRepository.create(user_id, input(%{name: "first"}), 1)
    {:ok, id2} = TaskRepository.create(user_id, input(%{name: "second"}), 1)
    {:ok, id3} = TaskRepository.create(user_id, input(%{name: "third"}), 1)

    page1 = get("/external/v1/tasks?user_id=#{user_id}&page=1&page_size=2", token)
    assert page1.status == 200
    assert page1.body["page"] == 1
    assert page1.body["page_size"] == 2
    assert page1.body["total"] == 3
    assert length(page1.body["tasks"]) == 2

    page2 = get("/external/v1/tasks?user_id=#{user_id}&page=2&page_size=2", token)
    assert page2.status == 200
    assert length(page2.body["tasks"]) == 1

    all_ids = Enum.map(page1.body["tasks"] ++ page2.body["tasks"], & &1["id"])
    assert Enum.sort(all_ids) == Enum.sort([id1, id2, id3])
  end

  test "cursor pagination chains to a null next_cursor at the end", %{user_id: user_id, private_key: private_key} do
    token = valid_token(private_key)
    {:ok, _id1} = TaskRepository.create(user_id, input(%{name: "c1"}), 1)
    {:ok, _id2} = TaskRepository.create(user_id, input(%{name: "c2"}), 1)

    :ok = flip_flag_on()
    on_exit(fn -> restore_flag() end)
    :ok = BackendElixir.Flags.FeatureFlagPoller.refresh()

    first = get("/external/v1/tasks?user_id=#{user_id}&limit=1", token)
    assert first.status == 200
    assert first.body["limit"] == 1
    assert length(first.body["tasks"]) == 1
    refute is_nil(first.body["next_cursor"])

    second = get("/external/v1/tasks?user_id=#{user_id}&cursor=#{first.body["next_cursor"]}&limit=1", token)
    assert second.status == 200
    assert length(second.body["tasks"]) == 1

    third = get("/external/v1/tasks?user_id=#{user_id}&cursor=#{second.body["next_cursor"]}&limit=1", token)
    assert third.status == 200
    assert third.body["tasks"] == []
    assert is_nil(third.body["next_cursor"])
  end

  defp flip_flag_on do
    {:ok, _} =
      Ecto.Adapters.SQL.query(Repo, "UPDATE feature_flags SET enabled = 1, default_variation = 'on' WHERE flag_key = ?", [@flag_key])

    :ok
  end

  defp restore_flag do
    {:ok, _} =
      Ecto.Adapters.SQL.query(Repo, "UPDATE feature_flags SET enabled = 1, default_variation = 'off' WHERE flag_key = ?", [@flag_key])

    :ok = BackendElixir.Flags.FeatureFlagPoller.refresh()
  end
end
