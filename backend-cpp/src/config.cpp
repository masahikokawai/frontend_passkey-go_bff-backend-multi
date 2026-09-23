#include "config.hpp"

#include <cstdlib>

namespace backend_cpp {

namespace {
std::string EnvOr(const char* name, std::string fallback) {
  const char* v = std::getenv(name);
  return v ? std::string(v) : fallback;
}
}  // namespace

Config Config::FromEnv() {
  Config cfg;
  cfg.http_addr = EnvOr("HTTP_ADDR", cfg.http_addr);
  cfg.db_host = EnvOr("DB_HOST", cfg.db_host);
  cfg.db_port = static_cast<uint16_t>(std::stoi(EnvOr("DB_PORT", "13306")));
  cfg.db_user = EnvOr("DB_USER", cfg.db_user);
  cfg.db_password = EnvOr("DB_PASSWORD", cfg.db_password);
  cfg.db_schema = EnvOr("DB_SCHEMA", cfg.db_schema);
  cfg.db_pool_size = std::stoi(EnvOr("DB_POOL_SIZE", "10"));
  cfg.grpc_addr = EnvOr("GRPC_ADDR", cfg.grpc_addr);
  cfg.external_http_addr = EnvOr("EXTERNAL_HTTP_ADDR", cfg.external_http_addr);
  cfg.keycloak_issuer = EnvOr("KEYCLOAK_ISSUER", cfg.keycloak_issuer);
  cfg.expected_audience = EnvOr("EXPECTED_AUDIENCE", cfg.expected_audience);
  cfg.external_api_client_id = EnvOr("EXTERNAL_API_CLIENT_ID", cfg.external_api_client_id);
  cfg.local_hmac_secret = EnvOr("LOCAL_AUTH_HMAC_SECRET", cfg.local_hmac_secret);
  cfg.local_rsa_jwks_url = EnvOr("LOCAL_AUTH_RSA_JWKS_URL", cfg.local_rsa_jwks_url);
  return cfg;
}

}  // namespace backend_cpp
