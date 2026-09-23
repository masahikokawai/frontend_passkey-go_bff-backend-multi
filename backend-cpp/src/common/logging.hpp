#pragma once

#include <string>

namespace backend_cpp::common {

// 最小限のログレベル制御。サードパーティのロギングライブラリは使わず、
// 環境変数LOG_LEVEL(debug/info/warn/error、既定info)でデバッグ行の出力有無だけを
// 切り替えるシンプルな設計にしている(backend-cのsrc/common/log.h・log_debugfと同じ考え方の
// C++版。C++側もC側と同じく「デバッグ行を出すか否か」の1点のみを制御し、
// warn/errorレベル自体の出し分けは行わない、学習用途としての簡略化)

// プロセス起動時に一度だけ呼ぶこと(main()冒頭、Config::FromEnv()の直後)。
// LOG_LEVEL環境変数の値(Config::log_level)を受け取り、以後のLogDebugの出力有無を決定する
void LogModuleInit(const std::string& log_level);

// LOG_LEVEL=debugのときだけ標準出力に1行出す(末尾の改行は本関数側で付与するため、
// 呼び出し側はline末尾に改行を含めないこと)
void LogDebug(const std::string& line);

// LOG_LEVELの値に関係なく常に標準出力に1行出す(既存のgRPCの
// grpc method=... status=... duration_ms=...(src/grpc/task_grpc_service.cpp、既存)と
// 同じ「リクエスト単位のINFOサマリは常時出力」という扱い。末尾の改行は本関数側で付与する
void LogInfo(const std::string& line);

}  // namespace backend_cpp::common
