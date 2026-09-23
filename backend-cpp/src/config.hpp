#pragma once

#include <cstdint>
#include <string>

namespace backend_cpp {

struct Config {
  std::string http_addr = "8105";  // HTTP_ADDR: ポートのみ受け付ける(簡略化)
  std::string db_host = "127.0.0.1";
  uint16_t db_port = 13306;
  std::string db_user = "root";
  std::string db_password;
  std::string db_schema = "bff_gin_development";
  int db_pool_size = 10;  // コネクションプール = DBスレッドプールと同数(README.md参照)

  std::string grpc_addr = "9099";
  std::string external_http_addr = "8109";
  std::string keycloak_issuer = "http://localhost:8082/realms/training";
  std::string expected_audience = "backend";
  std::string external_api_client_id = "external-api-client";
  std::string local_hmac_secret = "local-dev-hmac-shared-secret-change-me";
  std::string local_rsa_jwks_url = "http://localhost:8080/.well-known/jwks.json";

  static Config FromEnv();
};

}  // namespace backend_cpp
