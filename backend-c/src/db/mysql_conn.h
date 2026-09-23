#ifndef DB_MYSQL_CONN_H
#define DB_MYSQL_CONN_H

#include <mysql/mysql.h>

/*
 * コネクションプールを自作せず、CivetWebのワーカースレッドごとにMySQL接続を1本
 * スレッドローカルに保持する(pthread_key_tのデストラクタでスレッド終了時にmysql_closeが
 * 自動的に呼ばれる)。backend-cpp(RAIIによる自作プール)との対比: C++はオブジェクトの
 * スコープに紐づく自動解放、Cはスレッドのライフタイムに紐づく自動解放になる
 * (README.md「アーキテクチャ選定」節参照)
 */

/* プロセス起動時に一度だけ呼ぶこと(CivetWebのワーカースレッドを起動する前) */
int mysql_conn_module_init(const char *host, unsigned int port, const char *user,
                            const char *password, const char *db_name);

/*
 * 呼び出しスレッドに紐づくMySQL接続を返す(無ければ新規接続してキャッシュする)
 * 戻り値: 接続失敗時はNULL
 */
MYSQL *mysql_conn_get(void);

#endif /* DB_MYSQL_CONN_H */
