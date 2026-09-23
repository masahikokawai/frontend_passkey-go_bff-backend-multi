#ifndef COMMON_TIME_H
#define COMMON_TIME_H

#include <stddef.h>

/* 【過去に複数言語で見つかったバグの回帰防止】「今日」の判定は必ずUTC基準で行う
 * (サーバーのローカルタイムゾーンを絶対に使わない、gmtime_rのみ使用しlocaltime_rは使わない、
 * CONTRACT.mdセクション23.1参照)。out_bufは最低11バイト("YYYY-MM-DD"+NUL) */
void task_today_utc_iso(char *out_buf, size_t out_buf_len);

/* YYYY-MM-DD形式かつカレンダー上有効な日付かを確認する(例: 2026-02-30を弾く) */
int task_is_valid_iso_date(const char *s);

/* UTF-8のコードポイント数を数える(バイト数でもUTF-16コード単位数でもない)
 * 【過去に発見されたバグの回帰防止】backend-scala-http4sはUTF-16コード単位数で数えており、
 * サロゲートペア(絵文字等)を含む名前で20文字境界の判定がズレていた */
size_t task_utf8_codepoint_length(const char *s);

/* 現在時刻をMySQL DATETIME形式("YYYY-MM-DD HH:MM:SS")でUTC基準で書き出す
 * out_bufは最低20バイト */
void task_now_mysql_datetime(char *out_buf, size_t out_buf_len);

#endif /* COMMON_TIME_H */
