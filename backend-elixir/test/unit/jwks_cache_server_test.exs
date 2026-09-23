defmodule BackendElixir.Auth.JwksCacheServerTest do
  @moduledoc """
  【let it crashの実演、他言語には無いElixir固有のテストカバレッジ】JwksCacheServerが
  異常系(構文解析不能なJWKSレスポンス)でクラッシュした際、supervisorが実際に空の状態から
  再起動し、その後も通常通り機能し続けることを確認する。「未知のkidを拒否する」という
  通常のエラーハンドリング(test/unit/jwks_verifier_test.exsで別途カバー済み)とは区別し、
  ここではプロセス自体のクラッシュ+再起動というOTP固有の挙動そのものを検証する
  """
  use ExUnit.Case, async: false

  alias BackendElixir.Auth.JwksCacheServer
  alias BackendElixir.TestSupport.{MockJwksServer, TestTokenHelper}

  @server_name :test_jwks_cache_under_supervisor

  setup do
    {:ok, server_info} = MockJwksServer.start()
    MockJwksServer.reset_keys()

    child_spec = %{
      id: @server_name,
      start: {JwksCacheServer, :start_link, [[jwks_url: server_info.jwks_url, name: @server_name]]}
    }

    {:ok, sup_pid} = Supervisor.start_link([child_spec], strategy: :one_for_one)

    on_exit(fn ->
      MockJwksServer.stop(server_info)
      if Process.alive?(sup_pid), do: Supervisor.stop(sup_pid)
    end)

    %{sup_pid: sup_pid}
  end

  test "crashes on malformed JWKS response and the supervisor restarts it" do
    pid_before = Process.whereis(@server_name)
    assert is_pid(pid_before)
    assert Process.alive?(pid_before)

    # 【意図的なクラッシュ】"keys"フィールド自体が無いという、正常運用では起こり得ない
    # 明確に異常なJWKSレスポンスを与える。JwksCacheServerは防御的にエラーを握りつぶさず、
    # Map.fetch!/2の失敗でそのままクラッシュする(handle_call内、GenServerプロセスごと終了する)。
    # クラッシュしたサーバーへのGenServer.callはexitを送出するため、catch_exit/1で受け止める
    catch_exit(GenServer.call(@server_name, {:crash_with_malformed_response, %{}}))

    pid_after = wait_for_restart(pid_before, 50)
    assert is_pid(pid_after)
    assert pid_after != pid_before
    assert Process.alive?(pid_after)

    # 再起動後、キャッシュは空の状態から始まっているが、通常のリクエストには正常に応答できる
    # (再起動しただけで機能不全にはなっていないことの確認)
    assert JwksCacheServer.lookup(@server_name, "kid-after-restart") == nil

    private_key = TestTokenHelper.generate_rsa_key_pair()
    MockJwksServer.add_key("kid-after-restart", TestTokenHelper.public_jwk_fields(private_key))
    assert :ok = JwksCacheServer.refresh(@server_name)
    assert %Joken.Signer{} = JwksCacheServer.lookup(@server_name, "kid-after-restart")
  end

  defp wait_for_restart(_old_pid, 0), do: flunk("supervisor did not restart the crashed process in time")

  defp wait_for_restart(old_pid, attempts) do
    case Process.whereis(@server_name) do
      pid when is_pid(pid) and pid != old_pid ->
        pid

      _ ->
        Process.sleep(20)
        wait_for_restart(old_pid, attempts - 1)
    end
  end
end
