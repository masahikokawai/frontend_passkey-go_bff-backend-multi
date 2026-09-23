defmodule BackendElixir.Auth.HmacVerifier do
  @moduledoc """
  ローカルHMAC発行(iss=bff-gin-local-hmac)の検証。HS256、共有シークレット。
  `Joken.Signer.verify/2`は署名検証のみを行う(内部で:joseの検証を呼ぶだけで、exp/iss/aud等の
  クレーム自体の検証は行わない)ため、署名検証後にこの関数自身がiss/aud/expを明示的に確認する
  (他言語のHmacVerifierと同じ「ライブラリに全て任せず、境界の検証は自分の目でも確認する」という
  多層防御の設計)
  """

  alias BackendElixir.Auth.Dispatcher

  defstruct [:secret, :issuer, :audience]

  def new(secret, issuer, audience), do: %__MODULE__{secret: secret, issuer: issuer, audience: audience}

  def verify(%__MODULE__{} = v, token) do
    with {:ok, header} <- Joken.peek_header(token),
         :ok <- check_alg(header, "HS256"),
         {:ok, claims} <- verify_signature(v.secret, token),
         :ok <- check_iss(claims, v.issuer),
         :ok <- check_aud(claims, v.audience),
         :ok <- check_exp(claims) do
      Dispatcher.claims_from_map(claims)
    end
  end

  defp verify_signature(secret, token) do
    signer = Joken.Signer.create("HS256", secret)

    case Joken.Signer.verify(token, signer) do
      {:ok, claims} -> {:ok, claims}
      {:error, _reason} -> {:error, :signature_verification_failed}
    end
  end

  # 【セキュリティ上の確認、backend-java/backend-kotlin/backend-pythonと同じ観点】
  # Joken.Signer.verify/2自体はSignerに設定されたalgでのみ検証を試みるため、algが違えば
  # 署名検証で自然に失敗するはずだが、アルゴリズム混同攻撃への多層防御として、ヘッダのalgが
  # 期待するアルゴリズム(HS256)と完全一致することも独立して明示的に確認する
  defp check_alg(%{"alg" => alg}, expected) when alg == expected, do: :ok
  defp check_alg(_header, _expected), do: {:error, :unexpected_alg}

  defp check_iss(%{"iss" => iss}, expected) when iss == expected, do: :ok
  defp check_iss(_claims, _expected), do: {:error, :unexpected_issuer}

  # audはRFC7519上、文字列1個または文字列配列のどちらもありうる(他言語のAudienceMatchesと同じ)
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
