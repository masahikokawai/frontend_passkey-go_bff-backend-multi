defmodule BackendElixir.Auth.JwksVerifier do
  @moduledoc """
  Keycloak発行・ローカルRSA発行(iss=bff-gin-local-rsa)共通のJWKSベース検証。RS256。
  kidごとの公開鍵キャッシュは自分では持たず、`JwksCacheServer`(GenServer)に問い合わせる。
  未知のkidが来たときだけ再取得を依頼する「kid不一致時のみ再取得」戦略は他言語と同じ
  (backend(Go)のjwks.go・backend-c/backend-cpp/backend-java/backend-kotlin/backend-pythonと同じ)
  """

  alias BackendElixir.Auth.{Dispatcher, JwksCacheServer}

  defstruct [:cache_server, :issuer, :audience]

  def new(cache_server, issuer, audience) do
    %__MODULE__{cache_server: cache_server, issuer: issuer, audience: audience}
  end

  def verify(%__MODULE__{} = v, token) do
    with {:ok, header} <- Joken.peek_header(token),
         :ok <- check_alg(header, "RS256"),
         {:ok, kid} <- fetch_kid(header),
         {:ok, signer} <- lookup_or_refresh(v.cache_server, kid),
         {:ok, claims} <- verify_signature(signer, token),
         :ok <- check_iss(claims, v.issuer),
         :ok <- check_aud(claims, v.audience),
         :ok <- check_exp(claims) do
      Dispatcher.claims_from_map(claims)
    end
  end

  defp fetch_kid(%{"kid" => kid}) when is_binary(kid), do: {:ok, kid}
  defp fetch_kid(_header), do: {:error, :missing_kid}

  defp lookup_or_refresh(cache_server, kid) do
    case JwksCacheServer.lookup(cache_server, kid) do
      nil ->
        with :ok <- JwksCacheServer.refresh(cache_server) do
          case JwksCacheServer.lookup(cache_server, kid) do
            nil -> {:error, :unknown_kid}
            signer -> {:ok, signer}
          end
        end

      signer ->
        {:ok, signer}
    end
  end

  defp verify_signature(signer, token) do
    case Joken.Signer.verify(token, signer) do
      {:ok, claims} -> {:ok, claims}
      {:error, _reason} -> {:error, :signature_verification_failed}
    end
  end

  defp check_alg(%{"alg" => alg}, expected) when alg == expected, do: :ok
  defp check_alg(_header, _expected), do: {:error, :unexpected_alg}

  defp check_iss(%{"iss" => iss}, expected) when iss == expected, do: :ok
  defp check_iss(_claims, _expected), do: {:error, :unexpected_issuer}

  defp check_aud(%{"aud" => aud}, expected) when is_binary(aud) and aud == expected, do: :ok
  defp check_aud(%{"aud" => aud}, expected) when is_list(aud) do
    if expected in aud, do: :ok, else: {:error, :unexpected_audience}
  end
  defp check_aud(_claims, _expected), do: {:error, :unexpected_audience}

  defp check_exp(%{"exp" => exp}) when is_integer(exp) do
    if exp > System.system_time(:second), do: :ok, else: {:error, :token_expired}
  end
  defp check_exp(_claims), do: {:error, :missing_exp}
end
