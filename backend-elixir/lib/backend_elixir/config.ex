defmodule BackendElixir.Config do
  @moduledoc """
  backend-java/backend-kotlin/backend-python/backend-rustのenv var名・既定値と揃えている
  (同じdocker-compose環境の上で比較実行できるようにするため)
  """

  defstruct [
    :http_addr,
    :grpc_addr,
    :external_http_addr,
    :db_host,
    :db_port,
    :db_user,
    :db_password,
    :db_schema,
    :keycloak_issuer,
    :keycloak_jwks_url,
    :expected_audience,
    :local_hmac_secret,
    :local_rsa_jwks_url,
    :external_api_client_id,
    :log_level
  ]

  def from_env do
    keycloak_issuer = env_or("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training")

    keycloak_jwks_url =
      case System.get_env("KEYCLOAK_JWKS_URL") do
        nil -> keycloak_issuer <> "/protocol/openid-connect/certs"
        "" -> keycloak_issuer <> "/protocol/openid-connect/certs"
        v -> v
      end

    %__MODULE__{
      http_addr: env_or("HTTP_ADDR", "8117"),
      grpc_addr: env_or("GRPC_ADDR", "9104"),
      external_http_addr: env_or("EXTERNAL_HTTP_ADDR", "8118"),
      db_host: env_or("DB_HOST", "127.0.0.1"),
      db_port: String.to_integer(env_or("DB_PORT", "13306")),
      db_user: env_or("DB_USER", "root"),
      db_password: env_or("DB_PASSWORD", ""),
      db_schema: env_or("DB_SCHEMA", "bff_gin_development"),
      keycloak_issuer: keycloak_issuer,
      keycloak_jwks_url: keycloak_jwks_url,
      expected_audience: env_or("EXPECTED_AUDIENCE", "backend"),
      local_hmac_secret: env_or("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me"),
      local_rsa_jwks_url: env_or("LOCAL_AUTH_RSA_JWKS_URL", "http://localhost:8080/.well-known/jwks.json"),
      external_api_client_id: env_or("EXTERNAL_API_CLIENT_ID", "external-api-client"),
      log_level: env_or("LOG_LEVEL", "info")
    }
  end

  defp env_or(key, fallback) do
    case System.get_env(key) do
      nil -> fallback
      "" -> fallback
      v -> v
    end
  end
end
