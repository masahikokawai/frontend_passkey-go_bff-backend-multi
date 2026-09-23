defmodule BackendElixir.TestSupport.MockJwksServer.Plug do
  @moduledoc false
  use Plug.Router

  plug :match
  plug :dispatch

  get "/jwks" do
    keys = :persistent_term.get(:mock_jwks_keys, [])
    body = Jason.encode!(%{"keys" => keys})

    conn
    |> put_resp_content_type("application/json")
    |> send_resp(200, body)
  end
end

defmodule BackendElixir.TestSupport.MockJwksServer do
  @moduledoc """
  テスト専用の自プロセス内蔵JWKSサーバー(backend-java/backend-kotlin/backend-python/backend-c/
  backend-cppのモックJWKSサーバーと同じ役割)。実際にKeycloak/bffを起動せずにJwksVerifier/
  JwksCacheServerのRS256検証・kidキャッシュ・未知kid時の再取得ロジックを検証できる。
  本番実装(Plug.Router+Cowboy)と同じライブラリをそのままテスト用の使い捨てモックとしても
  再利用する(「本番はPlug/Cowboy、Verifierが叩く先はただのHTTPサーバーであれば何でもよい」
  という点を示す、backend-pythonが標準ライブラリのhttp.serverを使ったのと同じ考え方)
  """

  alias BackendElixir.TestSupport.MockJwksServer.Plug, as: MockPlug

  def start do
    ref = make_ref()
    {:ok, _pid} = Plug.Cowboy.http(MockPlug, [], ref: ref, port: 0)
    port = :ranch.get_port(ref)
    {:ok, %{ref: ref, port: port, jwks_url: "http://127.0.0.1:#{port}/jwks"}}
  end

  def stop(%{ref: ref}) do
    Plug.Cowboy.shutdown(ref)
  end

  @doc """
  :persistent_termでキー一覧を保持する(テスト実行プロセスとCowboyのリクエストハンドラは
  別プロセスのため、プロセス辞書ではなく全プロセス共有の状態として持つ、テスト専用の簡略化)
  """
  def add_key(kid, jwk_public_fields) do
    existing = :persistent_term.get(:mock_jwks_keys, [])
    key = Map.merge(%{"kty" => "RSA", "kid" => kid, "use" => "sig"}, jwk_public_fields)
    :persistent_term.put(:mock_jwks_keys, existing ++ [key])
  end

  def reset_keys do
    :persistent_term.put(:mock_jwks_keys, [])
  end
end
