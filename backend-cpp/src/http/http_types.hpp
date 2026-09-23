#pragma once

#include <map>
#include <string>

namespace backend_cpp::http {

// Beastの型に依存しない、Handler層向けの単純なリクエスト/レスポンス表現
// (Transport層とApplication層を分離する、README.md参照)
struct HttpRequest {
  std::string method;
  std::string target;  // パス+クエリ文字列
  std::string body;
  std::map<std::string, std::string> headers;  // 小文字化したヘッダ名をキーにする
};

struct HttpResponse {
  int status = 200;
  std::string content_type = "application/json";
  std::string body;

  static HttpResponse Json(int status, std::string json_body) {
    return HttpResponse{status, "application/json", std::move(json_body)};
  }
  static HttpResponse NoContent() { return HttpResponse{204, "application/json", ""}; }
};

}  // namespace backend_cpp::http
