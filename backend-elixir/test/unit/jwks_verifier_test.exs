defmodule BackendElixir.Auth.JwksVerifierTest do
  use ExUnit.Case, async: false

  alias BackendElixir.Auth.{JwksCacheServer, JwksVerifier}
  alias BackendElixir.TestSupport.{MockJwksServer, TestTokenHelper}

  @issuer "http://localhost:8082/realms/training"
  @audience "backend"

  setup do
    {:ok, server_info} = MockJwksServer.start()
    MockJwksServer.reset_keys()

    {:ok, cache_pid} = JwksCacheServer.start_link(jwks_url: server_info.jwks_url)

    on_exit(fn ->
      MockJwksServer.stop(server_info)
    end)

    %{server_info: server_info, cache_pid: cache_pid}
  end

  defp add_test_key(kid) do
    private_key = TestTokenHelper.generate_rsa_key_pair()
    MockJwksServer.add_key(kid, TestTokenHelper.public_jwk_fields(private_key))
    private_key
  end

  test "accepts valid token signed with known key", %{cache_pid: cache_pid} do
    private_key = add_test_key("kid-1")
    token = TestTokenHelper.make_rsa_token(private_key, "kid-1", @issuer, @audience, "99", 3600)
    verifier = JwksVerifier.new(cache_pid, @issuer, @audience)

    assert {:ok, claims} = JwksVerifier.verify(verifier, token)
    assert claims.sub == "99"
  end

  test "unknown_kid triggers refresh then succeeds if now present", %{cache_pid: cache_pid} do
    verifier = JwksVerifier.new(cache_pid, @issuer, @audience)
    # まだJWKSに何も登録されていない状態でリクエストが来た後にキーを追加する、という
    # 「未知kid -> 再取得 -> 見つかる」の流れを再現する
    private_key = TestTokenHelper.generate_rsa_key_pair()
    token = TestTokenHelper.make_rsa_token(private_key, "kid-late", @issuer, @audience, "5", 3600)
    MockJwksServer.add_key("kid-late", TestTokenHelper.public_jwk_fields(private_key))

    assert {:ok, claims} = JwksVerifier.verify(verifier, token)
    assert claims.sub == "5"
  end

  test "unknown kid still unknown after refresh is rejected", %{cache_pid: cache_pid} do
    private_key = TestTokenHelper.generate_rsa_key_pair()
    token = TestTokenHelper.make_rsa_token(private_key, "kid-never-registered", @issuer, @audience, "1", 3600)
    verifier = JwksVerifier.new(cache_pid, @issuer, @audience)

    assert {:error, :unknown_kid} = JwksVerifier.verify(verifier, token)
  end

  test "wrong issuer is rejected even with valid signature", %{cache_pid: cache_pid} do
    private_key = add_test_key("kid-2")
    token = TestTokenHelper.make_rsa_token(private_key, "kid-2", "unexpected-issuer", @audience, "1", 3600)
    verifier = JwksVerifier.new(cache_pid, @issuer, @audience)

    assert {:error, _reason} = JwksVerifier.verify(verifier, token)
  end

  test "wrong audience is rejected", %{cache_pid: cache_pid} do
    private_key = add_test_key("kid-3")
    token = TestTokenHelper.make_rsa_token(private_key, "kid-3", @issuer, "someone-else", "1", 3600)
    verifier = JwksVerifier.new(cache_pid, @issuer, @audience)

    assert {:error, _reason} = JwksVerifier.verify(verifier, token)
  end

  test "expired token is rejected", %{cache_pid: cache_pid} do
    private_key = add_test_key("kid-4")
    token = TestTokenHelper.make_rsa_token(private_key, "kid-4", @issuer, @audience, "1", -3600)
    verifier = JwksVerifier.new(cache_pid, @issuer, @audience)

    assert {:error, _reason} = JwksVerifier.verify(verifier, token)
  end

  test "rejects unexpected algorithm", %{cache_pid: cache_pid} do
    _private_key = add_test_key("kid-5")
    # RS256を期待しているVerifierに対し、HS256の(全く別の鍵体系の)トークンを送る
    token = TestTokenHelper.make_hmac_token("irrelevant-secret", @issuer, @audience, "1", 3600)
    verifier = JwksVerifier.new(cache_pid, @issuer, @audience)

    assert {:error, _reason} = JwksVerifier.verify(verifier, token)
  end
end
