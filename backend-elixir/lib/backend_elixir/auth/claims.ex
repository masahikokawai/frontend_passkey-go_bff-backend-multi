defmodule BackendElixir.Auth.Claims do
  @moduledoc """
  user_id解決(UserResolver)に必要な最小限のクレームに、外部公開API向けの`azp`を追加している。
  `azp`(authorized party)はClient Credentials Grantで発行されたトークンのクライアントIDを表し、
  外部公開API(External.Handler)がこの値をEXTERNAL_API_CLIENT_IDと比較する(内部REST/gRPCの
  user_id解決には使わない)
  """
  defstruct [:sub, :iss, :azp]
end
