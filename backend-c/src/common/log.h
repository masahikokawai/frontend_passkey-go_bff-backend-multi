#ifndef COMMON_LOG_H
#define COMMON_LOG_H

/*
 * 最小限のログレベル制御。サードパーティのロギングライブラリは使わず、
 * 環境変数LOG_LEVEL(debug/info/warn/error、既定info)で
 * デバッグログの出力有無だけを切り替えるシンプルな設計にしている
 * (backend(Go)・bff・gateway/goと同じ環境変数名だが、C側は「デバッグ行を出すか否か」
 * という1点のみ制御し、warn/errorレベル自体の出し分けは行わない、学習用途としての簡略化)
 */

/* プロセス起動時に一度だけ呼ぶこと。LOG_LEVEL環境変数を読み、以後のlog_debugfの
 * 出力有無を決定する */
void log_module_init(void);

/* LOG_LEVEL=debugのときだけ標準出力に1行出す(printfと同じ書式指定子が使える) */
void log_debugf(const char *fmt, ...);

#endif /* COMMON_LOG_H */
