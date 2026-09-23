defmodule BackendElixir.Rest.RequestLogger do
  @moduledoc """
  REST v1・外部公開APIの両リスナー共通のリクエスト単位ロギングplug。
  gRPCの`grpc method=... status=... duration_ms=...`(TaskService.logged/3、
  backend-java/backend-kotlin/backend-pythonのgrpcログと同じ形式)に合わせ、
  REST側も同じkey=value形式で1リクエスト1行のINFOログを出す。

  `register_before_send`(Plugが実際にレスポンスを送信する直前に呼ばれるコールバック)で
  `conn.status`を読むのがポイント。どのルートハンドラが処理したか・途中でエラーになったかに
  関わらず、実際に送信される最終的なステータスコードを直接見るため、ハンドラ側の分岐を
  ロギングのために複製する必要がない
  """
  @behaviour Plug
  require Logger

  @impl Plug
  def init(opts), do: Keyword.fetch!(opts, :label)

  @impl Plug
  def call(conn, label) do
    start = System.monotonic_time(:millisecond)

    Plug.Conn.register_before_send(conn, fn conn ->
      duration_ms = System.monotonic_time(:millisecond) - start

      Logger.info(
        "#{label} method=#{conn.method} path=#{conn.request_path} status=#{conn.status} duration_ms=#{duration_ms}"
      )

      conn
    end)
  end
end
