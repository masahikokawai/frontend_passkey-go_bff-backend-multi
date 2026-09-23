#include "flags/feature_flag_poller.h"

#include <mysql/mysql.h>
#include <pthread.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

#include "db/mysql_conn.h"

/* flag_keyは現状8個(feature_flagsテーブル実データ)で今後も学習用途の範囲でしか増えない
 * ため、dispatcher.cのissuer一覧と同じ考え方で固定長配列+線形探索にしている */
#define FLAG_POLLER_MAX_ENTRIES 32
#define FLAG_KEY_LEN 128
#define FLAG_VARIATION_LEN 64

typedef struct {
    char flag_key[FLAG_KEY_LEN];
    int enabled;
    char default_variation[FLAG_VARIATION_LEN];
} FlagEntry;

static FlagEntry g_entries[FLAG_POLLER_MAX_ENTRIES];
static size_t g_entry_count = 0;
static pthread_mutex_t g_mutex = PTHREAD_MUTEX_INITIALIZER;

static pthread_t g_thread;
static volatile int g_stop = 0;
static int g_started = 0;

/*
 * 【backend-cppとの設計対比】backend-cpp(src/flags/feature_flag_poller.cpp)は
 * ポーリングのたびにmysql_init/mysql_real_connect/mysql_close(接続の生成・破棄)を
 * 繰り返す。これはC++版のコネクションプール(RAII、リクエスト単位でAcquire/Release)が
 * 「長寿命のバックグラウンドスレッド専用の1本だけの接続」という用途にそのまま使えない
 * ためのやむを得ない設計。
 * backend-cは元々スレッドごとに1本の接続をpthread_key_tで使い回す設計
 * (db/mysql_conn.hのmysql_conn_get、CivetWebのワーカースレッド用に作った仕組み)を
 * 持っており、これはCivetWebのワーカースレッドに限らず「呼び出したスレッドに接続を
 * 紐づける」という汎用の仕組みなので、このポーリング専用スレッドからもそのまま
 * mysql_conn_get()を呼べば良い。接続はこのスレッドが生きている間(=プロセス終了まで)
 * 使い回され、MYSQL_OPT_RECONNECTにより一時的な切断からも自動復帰する。
 * 「スレッドローカル接続」という設計そのものが、C++のRAIIプールより長寿命スレッドに
 * 対して自然に適合する、という一例になっている
 */
static void poll_once(void) {
    MYSQL *conn = mysql_conn_get();
    if (conn == NULL) return;

    const char *sql = "SELECT flag_key, enabled, default_variation FROM feature_flags";
    if (mysql_query(conn, sql) != 0) {
        fprintf(stderr, "level=ERROR msg=\"feature_flagsクエリ失敗\" error=\"%s\"\n",
                mysql_error(conn));
        return;
    }
    MYSQL_RES *res = mysql_store_result(conn);
    if (res == NULL) return;

    FlagEntry new_entries[FLAG_POLLER_MAX_ENTRIES];
    size_t count = 0;
    MYSQL_ROW row;
    while (count < FLAG_POLLER_MAX_ENTRIES && (row = mysql_fetch_row(res)) != NULL) {
        snprintf(new_entries[count].flag_key, FLAG_KEY_LEN, "%s", row[0] != NULL ? row[0] : "");
        new_entries[count].enabled = (row[1] != NULL && strcmp(row[1], "1") == 0);
        snprintf(new_entries[count].default_variation, FLAG_VARIATION_LEN, "%s",
                 row[2] != NULL ? row[2] : "");
        count++;
    }
    mysql_free_result(res);

    pthread_mutex_lock(&g_mutex);
    memcpy(g_entries, new_entries, sizeof(FlagEntry) * count);
    g_entry_count = count;
    pthread_mutex_unlock(&g_mutex);
}

static void *run(void *arg) {
    (void)arg;
    while (!g_stop) {
        poll_once();
        /* 100ms刻みでg_stopを確認しながら合計10秒待つ(backend-cppと同じ間隔)。
         * こうしておくとfeature_flag_poller_stop()呼び出しから最大でも100ms程度で
         * 確実にスレッドを終了させられる(10秒丸ごとsleepすると終了が遅れる) */
        for (int i = 0; i < 100 && !g_stop; i++) {
            usleep(100 * 1000);
        }
    }
    return NULL;
}

int feature_flag_poller_start(void) {
    g_stop = 0;
    if (pthread_create(&g_thread, NULL, run, NULL) != 0) return -1;
    g_started = 1;
    return 0;
}

void feature_flag_poller_stop(void) {
    if (!g_started) return;
    g_stop = 1;
    pthread_join(g_thread, NULL);
    g_started = 0;
}

void feature_flag_poller_poll_once_for_test(void) { poll_once(); }

void feature_flag_poller_variation(const char *flag_key, const char *default_value, char *out_buf,
                                    size_t out_buf_len) {
    pthread_mutex_lock(&g_mutex);
    for (size_t i = 0; i < g_entry_count; i++) {
        if (strcmp(g_entries[i].flag_key, flag_key) == 0) {
            const char *value = g_entries[i].enabled ? g_entries[i].default_variation
                                                       : default_value;
            snprintf(out_buf, out_buf_len, "%s", value);
            pthread_mutex_unlock(&g_mutex);
            return;
        }
    }
    pthread_mutex_unlock(&g_mutex);
    snprintf(out_buf, out_buf_len, "%s", default_value);
}
