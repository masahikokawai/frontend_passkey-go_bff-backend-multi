defmodule BackendElixir.Auth.JwksHttpClient do
  @moduledoc """
  JWKSエンドポイントへの単純なHTTP GET。`Req`(モダンなElixir HTTPクライアント、BEAM上の
  `Finch`/`Mint`を基盤にする)を使う。GenServerの`handle_call`内から同期的に呼ぶだけなので、
  Java/Kotlin/Pythonのような「非同期ランタイムをブロックしないための特別な配慮」は不要
  (README.md「アーキテクチャ選定」節、Elixirの並行処理モデルの説明を参照)。

  テスト時は`JwksCacheServer`起動時にこのモジュール名の代わりにモック実装のモジュール名を
  渡すことで差し替えられる(Elixirにはinterfaceが無いため、共通の関数名を持つモジュールを
  値として渡すダックタイピング的な設計、Dispatcherと同じ考え方)
  """

  def fetch(url) do
    case Req.get(url, receive_timeout: 5_000) do
      {:ok, %Req.Response{status: 200, body: body}} when is_map(body) -> {:ok, body}
      {:ok, %Req.Response{status: 200, body: body}} when is_binary(body) -> Jason.decode(body)
      {:ok, %Req.Response{status: status}} -> {:error, {:http_status, status}}
      {:error, reason} -> {:error, reason}
    end
  end
end
