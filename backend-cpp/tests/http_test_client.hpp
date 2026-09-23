#pragma once

#include <map>
#include <string>

namespace backend_cpp::testutil {

struct HttpTestResponse {
  int status = 0;
  std::string body;
};

// テスト専用の単純な同期HTTP GETクライアント(auth::JwksVerifierのHttpGet同様、
// Boost::Beastのクライアント側APIで組み立てる)。headersはそのままリクエストヘッダとして送る
HttpTestResponse HttpGetWithHeaders(const std::string& host, unsigned short port,
                                     const std::string& target,
                                     const std::map<std::string, std::string>& headers);

}  // namespace backend_cpp::testutil
