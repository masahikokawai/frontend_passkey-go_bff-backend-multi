defmodule BackendElixir.TestSupport.TestTokenHelper do
  @moduledoc "backend-java/backend-kotlin/backend-pythonのtest_token_helper相当"

  def make_hmac_token(secret, iss, aud, sub, exp_offset_secs, alg \\ "HS256") do
    signer = Joken.Signer.create(alg, secret)
    claims = %{"sub" => sub, "iss" => iss, "aud" => aud, "exp" => System.system_time(:second) + exp_offset_secs}
    {:ok, token, _claims} = Joken.encode_and_sign(claims, signer)
    token
  end

  @doc """
  "alg":"none"は署名の要らない自己主張トークンになるため、必ず拒否されなければならない。
  Jokenは通常algをsigner経由で扱うため、"none"は手動で組み立てる
  (backend-java/backend-kotlin/backend-pythonの同名ヘルパーと同じ意図)
  """
  def make_alg_none_token(iss, aud, sub, exp_offset_secs) do
    header = %{"alg" => "none", "typ" => "JWT"}
    claims = %{"sub" => sub, "iss" => iss, "aud" => aud, "exp" => System.system_time(:second) + exp_offset_secs}
    header_b64 = b64url(Jason.encode!(header))
    payload_b64 = b64url(Jason.encode!(claims))
    "#{header_b64}.#{payload_b64}."
  end

  def generate_rsa_key_pair do
    :public_key.generate_key({:rsa, 2048, 65537})
  end

  def make_rsa_token(private_key, kid, iss, aud, sub, exp_offset_secs, alg \\ "RS256") do
    jwk = private_jwk(private_key)
    signer = Joken.Signer.create(alg, jwk, %{"kid" => kid})
    claims = %{"sub" => sub, "iss" => iss, "aud" => aud, "exp" => System.system_time(:second) + exp_offset_secs}
    {:ok, token, _claims} = Joken.encode_and_sign(claims, signer)
    token
  end

  @doc "外部公開API向け(azp=Client Credentials Grantのクライアントid)のRS256トークンを組み立てる"
  def make_rsa_token_with_azp(private_key, kid, iss, aud, sub, azp, exp_offset_secs, alg \\ "RS256") do
    jwk = private_jwk(private_key)
    signer = Joken.Signer.create(alg, jwk, %{"kid" => kid})

    claims = %{
      "sub" => sub,
      "iss" => iss,
      "aud" => aud,
      "azp" => azp,
      "exp" => System.system_time(:second) + exp_offset_secs
    }

    {:ok, token, _claims} = Joken.encode_and_sign(claims, signer)
    token
  end

  def public_jwk_fields(private_key) do
    {:RSAPrivateKey, _, modulus, exponent, _, _, _, _, _, _, _} = private_key
    %{"n" => b64url_uint(modulus), "e" => b64url_uint(exponent)}
  end

  defp private_jwk(private_key) do
    {:RSAPrivateKey, _, modulus, pub_exp, priv_exp, _, _, _, _, _, _} = private_key

    %{
      "kty" => "RSA",
      "n" => b64url_uint(modulus),
      "e" => b64url_uint(pub_exp),
      "d" => b64url_uint(priv_exp)
    }
  end

  defp b64url_uint(value) do
    value |> :binary.encode_unsigned() |> b64url()
  end

  defp b64url(data), do: Base.url_encode64(data, padding: false)
end
