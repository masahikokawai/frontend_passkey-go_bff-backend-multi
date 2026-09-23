defmodule BackendElixir.Auth.ExternalAuth do
  @moduledoc """
  外部公開API(External.Handler)専用の認証チェック(CONTRACT.mdセクション11
  「RequireExternalClientAuth」)。内部REST/gRPCが使うUserResolver.resolve/2とは異なり、
  ここではJWTのsubからuser_idを解決しない(呼び出し元が指定したuser_idクエリパラメータを
  そのまま信頼する設計、既知の制約としてREADME.mdに明記する)。代わりに以下2点を追加で確認する:

    1. issがローカル発行(bff-gin-local-hmac/bff-gin-local-rsa)でないこと
       (Client Credentials Grantで発行されたKeycloakトークンのみを受け付ける)
    2. claims.azpがEXTERNAL_API_CLIENT_IDと一致すること

  Dispatcher自体(署名検証)は内部APIと共有し、認証ロジックを複製しない
  (backend-c/backend-cpp/backend-java/backend-kotlin/backend-pythonのRequireExternalClientAuth
  相当と同じ設計)
  """

  alias BackendElixir.Auth.Dispatcher
  alias BackendElixir.Domain.TaskError

  def require_external_client(dispatcher, authorization_header, external_api_client_id) do
    with {:ok, token} <- strip_bearer(authorization_header),
         {:ok, claims} <- dispatcher_verify(dispatcher, token),
         false <- Dispatcher.local_issuer?(claims.iss),
         true <- claims.azp == external_api_client_id do
      {:ok, claims}
    else
      _ -> {:error, TaskError.unauthenticated()}
    end
  end

  defp strip_bearer("Bearer " <> token) when byte_size(token) > 0, do: {:ok, token}
  defp strip_bearer(_), do: {:error, :missing_authorization}

  defp dispatcher_verify(dispatcher, token) do
    case Dispatcher.verify(dispatcher, token) do
      {:ok, claims} -> {:ok, claims}
      {:error, _reason} -> {:error, :verification_failed}
    end
  end
end
