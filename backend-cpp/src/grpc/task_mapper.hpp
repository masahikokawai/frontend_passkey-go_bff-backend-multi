#pragma once

#include "domain/task.hpp"
#include "task.pb.h"

namespace backend_cpp::grpcservice {

// protobuf生成クラス(task::v1::Task)をハンドラで直接扱わず、ドメインモデル(domain::Task)との
// 変換をここに集約する(REST/gRPCでドメインを共有し、protoの都合をドメインへ漏らさない設計。
// README.md「gRPC」節参照)
task::v1::Task ToProto(const domain::Task& task);

}  // namespace backend_cpp::grpcservice
