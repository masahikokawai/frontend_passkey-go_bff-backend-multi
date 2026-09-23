#ifndef FLAGS_FEATURE_FLAG_POLLER_H
#define FLAGS_FEATURE_FLAG_POLLER_H

#include <stddef.h>

/*
 * backend(Go)・backend-rust(src/flags.rs)・backend-cpp(src/flags/feature_flag_poller.{hpp,cpp})と
 * 同じ設計: 専用のバックグラウンドスレッドがfeature_flagsテーブルを10秒間隔で直接ポーリングする
 * (bffのようなHTTPポーリングではなく、backend自身がこのFeature Flagを評価する。
 * 外部公開APIのパスにBFFは介在しないため、CONTRACT.mdセクション11の通りbackend側にも
 * 判定ロジックを持つ必要がある)。対象は`backend.external-tasks-pagination-v2`のみ
 */

/* プロセス起動時に一度だけ呼ぶこと(mysql_conn_module_init呼び出し後)。
 * 戻り値: 成功時0、pthread_create失敗時-1 */
int feature_flag_poller_start(void);

/* プロセス終了時に呼ぶ(スレッドの終了を待ち合わせる) */
void feature_flag_poller_stop(void);

/*
 * flag_keyの現在のvariationをout_bufへ書き込む(呼び出し側が確保したバッファに書く、
 * mallocされた文字列を返さないことで所有権の受け渡しを無くしている)。
 * enabled=falseの場合、またはflag_key未登録の場合はdefault_valueを書き込む
 */
void feature_flag_poller_variation(const char *flag_key, const char *default_value, char *out_buf,
                                    size_t out_buf_len);

/*
 * テスト専用: 10秒間隔のバックグラウンドポーリングを待たずに即座に1回ポーリングする
 * (tests/feature_flag_poller_test.c参照)。本番コード(main.c)からは呼ばないこと。
 * feature_flag_poller_start()を呼んでいなくても単独で使える(mysql_conn_module_init済みなら
 * mysql_conn_get()が動くため)
 */
void feature_flag_poller_poll_once_for_test(void);

#endif /* FLAGS_FEATURE_FLAG_POLLER_H */
