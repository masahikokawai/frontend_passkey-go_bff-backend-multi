defmodule BackendElixir.Auth.Dispatcher do
  @moduledoc """
  JWTの`iss`クレームで検証方式(Keycloak/ローカルHMAC/ローカルRSA)を振り分ける
  (backend(Go)のauthjwt.Dispatcher・backend-rust/backend-c/backend-cpp/backend-java/
  backend-kotlin/backend-pythonと同じ2段構造)。issの詐称は委譲先の署名検証で弾かれる:
  「振り分けのために覗く」ことと「検証を信頼する」ことは別、という2段階構造を維持する。

  検証方式(HmacVerifier/JwksVerifier)は`{module, arg}`のタプルとして登録する。verify/2は
  登録されたmoduleの`verify/2`関数を呼ぶ(Elixirにはinterfaceが無いため、共通の関数名を
  持つモジュールを値として保持するダックタイピング的な設計にする)
  """

  alias BackendElixir.Auth.Claims

  @local_hmac_issuer "bff-gin-local-hmac"
  @local_rsa_issuer "bff-gin-local-rsa"

  def local_hmac_issuer, do: @local_hmac_issuer
  def local_rsa_issuer, do: @local_rsa_issuer

  def local_issuer?(iss), do: iss in [@local_hmac_issuer, @local_rsa_issuer]

  def new, do: %{}

  def register(dispatcher, issuer, verifier_mod, verifier_arg) do
    Map.put(dispatcher, issuer, {verifier_mod, verifier_arg})
  end

  def verify(dispatcher, token) do
    with {:ok, [_header_b64, payload_b64, _sig_b64]} <- split(token),
         {:ok, payload} <- decode_payload(payload_b64),
         {:ok, iss} <- fetch_iss(payload),
         {:ok, {verifier_mod, verifier_arg}} <- lookup(dispatcher, iss) do
      verifier_mod.verify(verifier_arg, token)
    end
  end

  defp split(token) do
    case String.split(token, ".") do
      [_, _, _] = parts -> {:ok, parts}
      _ -> {:error, :malformed_token}
    end
  end

  defp decode_payload(payload_b64) do
    with {:ok, raw} <- Base.url_decode64(payload_b64, padding: false),
         {:ok, payload} <- Jason.decode(raw) do
      {:ok, payload}
    else
      _ -> {:error, :malformed_token}
    end
  end

  defp fetch_iss(%{"iss" => iss}) when is_binary(iss), do: {:ok, iss}
  defp fetch_iss(_), do: {:error, :unknown_issuer}

  defp lookup(dispatcher, iss) do
    case Map.fetch(dispatcher, iss) do
      {:ok, entry} -> {:ok, entry}
      :error -> {:error, :unknown_issuer}
    end
  end

  @doc false
  def claims_from_map(%{"sub" => sub, "iss" => iss} = payload) when is_binary(sub) and is_binary(iss) do
    azp = Map.get(payload, "azp", "")
    {:ok, %Claims{sub: sub, iss: iss, azp: azp}}
  end

  def claims_from_map(_), do: {:error, :missing_claims}
end
