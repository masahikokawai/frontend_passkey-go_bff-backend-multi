#include "db/mysql_conn.h"

#include <pthread.h>
#include <stdbool.h>
#include <stdio.h>
#include <string.h>

/* 【backend-cppでの実機検証で判明した既知の差異】このmysql-clientバージョンでは
 * my_bool型が削除されており、mysql_options(MYSQL_OPT_RECONNECT, ...)にはboolを渡す
 * (backend-cpp/README.md参照) */

static pthread_key_t g_conn_key;
static pthread_once_t g_key_once = PTHREAD_ONCE_INIT;

static char g_host[256];
static unsigned int g_port;
static char g_user[128];
static char g_password[128];
static char g_dbname[128];

/* pthread_key_createの第2引数: スレッド終了時に自動的に呼ばれる(pthreadの中では珍しい
 * 「自動解放」の仕組み。ただしスレッドが生きている間はずっと接続が生き続ける点に注意、
 * リクエスト単位のメモリ解放にはならないので、そちらはTask/TaskInputのcreate/destroyで
 * 別途手動管理する) */
static void destroy_thread_connection(void *ptr) {
    if (ptr != NULL) {
        mysql_close((MYSQL *)ptr);
    }
}

/*
 * 【メモリ/リソース管理のバッド/グッドプラクティス】pthread_key_createの第2引数(デストラクタ)に
 * NULLを渡すと、スレッド終了時に何も自動的には呼ばれなくなる。CivetWebはワーカースレッドを
 * 使い回すため即座には顕在化しないが、ワーカースレッドが動的に再作成される構成や
 * プロセスの寿命が長い構成では、スレッドが終了するたびにMYSQL接続(ソケットfd含む)が
 * リークし続ける(CWE-772、リソース解放漏れ):
 *
 *   pthread_key_create(&g_conn_key, NULL);  // ← スレッド終了時にmysql_closeが呼ばれない
 *
 * 修正後: destroy_thread_connectionを第2引数に渡すことで、スレッド終了時にpthread自身が
 * 自動的にmysql_close()を呼ぶ(C++版のRAIIとは異なり「スレッドのライフタイム」に
 * 紐づく自動解放になる点はREADME.md「アーキテクチャ選定」節参照)
 */
static void make_key(void) {
    pthread_key_create(&g_conn_key, destroy_thread_connection);
}

int mysql_conn_module_init(const char *host, unsigned int port, const char *user,
                            const char *password, const char *db_name) {
    pthread_once(&g_key_once, make_key);
    snprintf(g_host, sizeof(g_host), "%s", host);
    g_port = port;
    snprintf(g_user, sizeof(g_user), "%s", user);
    snprintf(g_password, sizeof(g_password), "%s", password != NULL ? password : "");
    snprintf(g_dbname, sizeof(g_dbname), "%s", db_name);
    return 0;
}

MYSQL *mysql_conn_get(void) {
    MYSQL *conn = (MYSQL *)pthread_getspecific(g_conn_key);
    if (conn != NULL) {
        return conn;
    }
    conn = mysql_init(NULL);
    if (conn == NULL) return NULL;
    bool reconnect = true;
    mysql_options(conn, MYSQL_OPT_RECONNECT, &reconnect);
    if (mysql_real_connect(conn, g_host, g_user, g_password, g_dbname, g_port, NULL, 0) == NULL) {
        fprintf(stderr, "mysql_real_connect failed: %s\n", mysql_error(conn));
        mysql_close(conn);
        return NULL;
    }
    pthread_setspecific(g_conn_key, conn);
    return conn;
}
