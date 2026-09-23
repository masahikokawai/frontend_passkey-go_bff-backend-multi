#include "grpc/task_mapper.hpp"

#include <ctime>

namespace backend_cpp::grpcservice {

namespace {

// MySQL DATETIME文字列("YYYY-MM-DD HH:MM:SS"、常にUTCとして保存されている、
// CONTRACT.mdセクション23.1のUTC統一方針)をprotobufのTimestampへ変換する
// (backend-rustのnaive_to_timestampと同じ扱い: naiveな日時をUTCとして解釈する)
google::protobuf::Timestamp MysqlDatetimeToTimestamp(const std::string& mysql_dt) {
  std::tm tm{};
  // "YYYY-MM-DD HH:MM:SS" を解析する。sscanfで十分(固定フォーマットのため)
  std::sscanf(mysql_dt.c_str(), "%d-%d-%d %d:%d:%d", &tm.tm_year, &tm.tm_mon, &tm.tm_mday,
              &tm.tm_hour, &tm.tm_min, &tm.tm_sec);
  tm.tm_year -= 1900;
  tm.tm_mon -= 1;
  // timegmはローカルタイムゾーンを一切見ず、tmをUTCとして解釈してepoch秒に変換する
  // (POSIX拡張、mktime+タイムゾーン計算のようなローカルタイムゾーン依存を避けるため採用)
  time_t seconds = timegm(&tm);
  google::protobuf::Timestamp ts;
  ts.set_seconds(static_cast<int64_t>(seconds));
  ts.set_nanos(0);
  return ts;
}

}  // namespace

task::v1::Task ToProto(const domain::Task& task) {
  task::v1::Task pb;
  pb.set_id(static_cast<uint64_t>(task.id));
  pb.set_name(task.name);
  if (task.description.has_value()) pb.set_description(*task.description);
  pb.set_status(domain::StatusToString(task.status));
  pb.set_finished_on(task.finished_on);
  for (const auto& l : task.labels) {
    auto* label = pb.add_labels();
    label->set_id(static_cast<uint64_t>(l.id));
    label->set_name(l.name);
  }
  *pb.mutable_created_at() = MysqlDatetimeToTimestamp(task.created_at);
  *pb.mutable_updated_at() = MysqlDatetimeToTimestamp(task.updated_at);
  return pb;
}

}  // namespace backend_cpp::grpcservice
