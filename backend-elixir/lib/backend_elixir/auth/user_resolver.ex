defmodule BackendElixir.Auth.UserResolver do
  @moduledoc """
  REST/gRPCの両トランスポートが共有するuser_id解決ロジック(認証ロジックを複製しない設計)。
  backend(Go)のresolveUserID・backend-java/backend-kotlin/backend-pythonのUserResolverと同じ分岐:
      - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの -> usersをidで検索
      - Keycloak発行のJWT: subはkeycloak_sub -> user_keycloaks経由で検索
  どちらも見つからなければuser_not_provisioned
  """

  alias BackendElixir.Auth.Dispatcher
  alias BackendElixir.Domain.TaskError
  alias BackendElixir.Repository.TaskRepository

  def resolve(dispatcher, authorization_header) do
    with {:ok, token} <- strip_bearer(authorization_header),
         {:ok, claims} <- dispatcher_verify(dispatcher, token) do
      resolve_user_id(claims)
    else
      _ -> {:error, TaskError.unauthorized()}
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

  defp resolve_user_id(claims) do
    if Dispatcher.local_issuer?(claims.iss) do
      resolve_local(claims.sub)
    else
      resolve_keycloak(claims.sub)
    end
  end

  defp resolve_local(sub) do
    case Integer.parse(sub) do
      {user_id, ""} ->
        case TaskRepository.find_user_by_id(user_id) do
          {:ok, user} -> {:ok, user.id}
          {:error, :not_found} -> {:error, TaskError.user_not_provisioned()}
        end

      _ ->
        {:error, TaskError.user_not_provisioned()}
    end
  end

  defp resolve_keycloak(keycloak_sub) do
    case TaskRepository.find_user_by_keycloak_sub(keycloak_sub) do
      {:ok, user} -> {:ok, user.id}
      {:error, :not_found} -> {:error, TaskError.user_not_provisioned()}
    end
  end
end
