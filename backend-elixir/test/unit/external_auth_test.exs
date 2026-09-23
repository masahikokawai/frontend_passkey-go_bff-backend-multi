defmodule BackendElixir.Auth.ExternalAuthTest do
  @moduledoc """
  外部公開API専用の認証チェック(ExternalAuth.require_external_client/3)を、実DBを使わずに
  モックJWKSサーバー(Keycloak相当)とローカルHMACのDispatcher経由で検証する
  """
  use ExUnit.Case, async: false

  alias BackendElixir.Auth.{Dispatcher, ExternalAuth, HmacVerifier, JwksCacheServer, JwksVerifier}
  alias BackendElixir.TestSupport.{MockJwksServer, TestTokenHelper}

  @keycloak_issuer "http://localhost:8082/realms/training"
  @audience "backend"
  @external_api_client_id "external-api-client"
  @hmac_secret "test-external-hmac-secret"

  setup do
    {:ok, server_info} = MockJwksServer.start()
    MockJwksServer.reset_keys()
    {:ok, cache_pid} = JwksCacheServer.start_link(jwks_url: server_info.jwks_url)

    dispatcher =
      Dispatcher.new()
      |> Dispatcher.register(
        Dispatcher.local_hmac_issuer(),
        HmacVerifier,
        HmacVerifier.new(@hmac_secret, Dispatcher.local_hmac_issuer(), @audience)
      )
      |> Dispatcher.register(@keycloak_issuer, JwksVerifier, JwksVerifier.new(cache_pid, @keycloak_issuer, @audience))

    on_exit(fn -> MockJwksServer.stop(server_info) end)

    %{dispatcher: dispatcher}
  end

  defp add_key(kid) do
    private_key = TestTokenHelper.generate_rsa_key_pair()
    MockJwksServer.add_key(kid, TestTokenHelper.public_jwk_fields(private_key))
    private_key
  end

  test "accepts a Keycloak-style token with matching azp", %{dispatcher: dispatcher} do
    private_key = add_key("kid-ext-1")
    token = TestTokenHelper.make_rsa_token_with_azp(private_key, "kid-ext-1", @keycloak_issuer, @audience, "user-sub", @external_api_client_id, 3600)

    assert {:ok, claims} = ExternalAuth.require_external_client(dispatcher, "Bearer " <> token, @external_api_client_id)
    assert claims.azp == @external_api_client_id
  end

  test "rejects a valid token with a mismatched azp", %{dispatcher: dispatcher} do
    private_key = add_key("kid-ext-2")
    token = TestTokenHelper.make_rsa_token_with_azp(private_key, "kid-ext-2", @keycloak_issuer, @audience, "user-sub", "some-other-client", 3600)

    assert {:error, %{kind: :unauthenticated}} =
             ExternalAuth.require_external_client(dispatcher, "Bearer " <> token, @external_api_client_id)
  end

  test "rejects a valid token with no azp claim at all", %{dispatcher: dispatcher} do
    private_key = add_key("kid-ext-3")
    token = TestTokenHelper.make_rsa_token(private_key, "kid-ext-3", @keycloak_issuer, @audience, "user-sub", 3600)

    assert {:error, %{kind: :unauthenticated}} =
             ExternalAuth.require_external_client(dispatcher, "Bearer " <> token, @external_api_client_id)
  end

  test "rejects a correctly-signed local HMAC token even with a matching azp claim", %{dispatcher: dispatcher} do
    # ローカル発行issuerは内部REST/gRPC専用であり、署名検証自体が正しく通っても
    # 外部公開APIでは受け付けない(CONTRACT.mdセクション11)
    token =
      TestTokenHelper.make_hmac_token(@hmac_secret, Dispatcher.local_hmac_issuer(), @audience, "1", 3600)

    assert {:error, %{kind: :unauthenticated}} =
             ExternalAuth.require_external_client(dispatcher, "Bearer " <> token, @external_api_client_id)
  end

  test "rejects when the authorization header is missing", %{dispatcher: dispatcher} do
    assert {:error, %{kind: :unauthenticated}} = ExternalAuth.require_external_client(dispatcher, nil, @external_api_client_id)
  end
end
