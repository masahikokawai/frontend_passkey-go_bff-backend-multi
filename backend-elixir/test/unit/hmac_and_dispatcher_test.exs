defmodule BackendElixir.Auth.HmacAndDispatcherTest do
  use ExUnit.Case, async: true

  alias BackendElixir.Auth.{Dispatcher, HmacVerifier}
  alias BackendElixir.TestSupport.TestTokenHelper

  @secret "test-secret"
  @issuer Dispatcher.local_hmac_issuer()
  @audience "backend"

  test "is_local_issuer matches hmac and rsa only" do
    assert Dispatcher.local_issuer?(Dispatcher.local_hmac_issuer())
    assert Dispatcher.local_issuer?(Dispatcher.local_rsa_issuer())
    refute Dispatcher.local_issuer?("http://localhost:8082/realms/training")
    refute Dispatcher.local_issuer?("")
  end

  test "hmac_verifier accepts valid token" do
    token = TestTokenHelper.make_hmac_token(@secret, @issuer, @audience, "42", 3600)
    verifier = HmacVerifier.new(@secret, @issuer, @audience)
    assert {:ok, claims} = HmacVerifier.verify(verifier, token)
    assert claims.sub == "42"
    assert claims.iss == @issuer
  end

  test "hmac_verifier rejects wrong secret" do
    token = TestTokenHelper.make_hmac_token(@secret, @issuer, @audience, "42", 3600)
    verifier = HmacVerifier.new("different-secret", @issuer, @audience)
    assert {:error, _reason} = HmacVerifier.verify(verifier, token)
  end

  test "hmac_verifier rejects expired token" do
    token = TestTokenHelper.make_hmac_token(@secret, @issuer, @audience, "42", -3600)
    verifier = HmacVerifier.new(@secret, @issuer, @audience)
    assert {:error, _reason} = HmacVerifier.verify(verifier, token)
  end

  test "hmac_verifier rejects wrong audience" do
    token = TestTokenHelper.make_hmac_token(@secret, @issuer, "someone-else", "42", 3600)
    verifier = HmacVerifier.new(@secret, @issuer, @audience)
    assert {:error, _reason} = HmacVerifier.verify(verifier, token)
  end

  test "hmac_verifier rejects wrong issuer" do
    token = TestTokenHelper.make_hmac_token(@secret, "unexpected-issuer", @audience, "42", 3600)
    verifier = HmacVerifier.new(@secret, @issuer, @audience)
    assert {:error, _reason} = HmacVerifier.verify(verifier, token)
  end

  test "hmac_verifier rejects unexpected algorithm" do
    token = TestTokenHelper.make_hmac_token(@secret, @issuer, @audience, "42", 3600, "HS384")
    verifier = HmacVerifier.new(@secret, @issuer, @audience)
    assert {:error, _reason} = HmacVerifier.verify(verifier, token)
  end

  test "hmac_verifier rejects alg none" do
    token = TestTokenHelper.make_alg_none_token(@issuer, @audience, "42", 3600)
    verifier = HmacVerifier.new(@secret, @issuer, @audience)
    assert {:error, _reason} = HmacVerifier.verify(verifier, token)
  end

  test "dispatcher routes by issuer and rejects unknown issuer" do
    dispatcher =
      Dispatcher.new()
      |> Dispatcher.register(@issuer, HmacVerifier, HmacVerifier.new(@secret, @issuer, @audience))

    good_token = TestTokenHelper.make_hmac_token(@secret, @issuer, @audience, "7", 3600)
    assert {:ok, claims} = Dispatcher.verify(dispatcher, good_token)
    assert claims.sub == "7"

    unknown_issuer_token = TestTokenHelper.make_hmac_token(@secret, "unknown-issuer", @audience, "7", 3600)
    assert {:error, :unknown_issuer} = Dispatcher.verify(dispatcher, unknown_issuer_token)
  end

  test "dispatcher rejects malformed token" do
    dispatcher =
      Dispatcher.new() |> Dispatcher.register(@issuer, HmacVerifier, HmacVerifier.new(@secret, @issuer, @audience))

    assert {:error, :malformed_token} = Dispatcher.verify(dispatcher, "not-a-jwt")
  end
end
