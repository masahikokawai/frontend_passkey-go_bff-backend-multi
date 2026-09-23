#pragma once

#include <string>

namespace backend_cpp::common {

// 【過去に複数言語で見つかったバグの回帰防止】「今日」の判定は必ずUTC基準で行う
// (サーバーのローカルタイムゾーンを絶対に使わない、CONTRACT.mdセクション23.1参照)
std::string TodayUtcIso();

// YYYY-MM-DD形式かつカレンダー上有効な日付かを確認する(例: 2026-02-30を弾く)
bool IsValidIsoDate(const std::string& s);

// UTF-8のコードポイント数を数える(バイト数ではない、UTF-16コード単位数でもない)
// 【過去に発見されたバグの回帰防止】backend-scala-http4sはUTF-16コード単位数で数えており、
// サロゲートペア(絵文字等)を含む名前で20文字境界の判定がズレていた
size_t Utf8CodepointLength(const std::string& s);

}  // namespace backend_cpp::common
