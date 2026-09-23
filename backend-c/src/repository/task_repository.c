#include "repository/task_repository.h"

#include <mysql/mysql.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "common/time.h"
#include "db/mysql_conn.h"

#define STR_BUF_LEN 1024

/* ---- 結果カラムを受け取るための固定長バッファ(id/name/description/status/finished_on/
 * created_at/updated_atの7列固定、backend-cppのResultRowと同じ形) ---- */
typedef struct {
    int64_t id;
    char name[STR_BUF_LEN];
    unsigned long name_len;
    char description[STR_BUF_LEN];
    unsigned long description_len;
    bool description_is_null;
    int64_t status;
    char finished_on[16];
    unsigned long finished_on_len;
    char created_at[32];
    unsigned long created_at_len;
    char updated_at[32];
    unsigned long updated_at_len;
} ResultRow;

static void bind_result_row(MYSQL_BIND *binds, ResultRow *row) {
    memset(binds, 0, sizeof(MYSQL_BIND) * 7);
    binds[0].buffer_type = MYSQL_TYPE_LONGLONG;
    binds[0].buffer = &row->id;
    binds[1].buffer_type = MYSQL_TYPE_STRING;
    binds[1].buffer = row->name;
    binds[1].buffer_length = STR_BUF_LEN;
    binds[1].length = &row->name_len;
    binds[2].buffer_type = MYSQL_TYPE_STRING;
    binds[2].buffer = row->description;
    binds[2].buffer_length = STR_BUF_LEN;
    binds[2].length = &row->description_len;
    binds[2].is_null = (bool *)&row->description_is_null;
    binds[3].buffer_type = MYSQL_TYPE_LONGLONG;
    binds[3].buffer = &row->status;
    binds[4].buffer_type = MYSQL_TYPE_STRING;
    binds[4].buffer = row->finished_on;
    binds[4].buffer_length = sizeof(row->finished_on);
    binds[4].length = &row->finished_on_len;
    binds[5].buffer_type = MYSQL_TYPE_STRING;
    binds[5].buffer = row->created_at;
    binds[5].buffer_length = sizeof(row->created_at);
    binds[5].length = &row->created_at_len;
    binds[6].buffer_type = MYSQL_TYPE_STRING;
    binds[6].buffer = row->updated_at;
    binds[6].buffer_length = sizeof(row->updated_at);
    binds[6].length = &row->updated_at_len;
}

static int row_to_task(const ResultRow *row, Task *t) {
    t->id = row->id;
    char name_buf[STR_BUF_LEN + 1];
    memcpy(name_buf, row->name, row->name_len);
    name_buf[row->name_len] = '\0';
    if (task_set_name(t, name_buf) != 0) return -1;

    if (!row->description_is_null) {
        char desc_buf[STR_BUF_LEN + 1];
        memcpy(desc_buf, row->description, row->description_len);
        desc_buf[row->description_len] = '\0';
        if (task_set_description(t, desc_buf) != 0) return -1;
    }
    t->status = (TaskStatus)row->status;
    snprintf(t->finished_on, sizeof(t->finished_on), "%.*s", (int)row->finished_on_len,
             row->finished_on);
    snprintf(t->created_at, sizeof(t->created_at), "%.*s", (int)row->created_at_len,
             row->created_at);
    snprintf(t->updated_at, sizeof(t->updated_at), "%.*s", (int)row->updated_at_len,
             row->updated_at);
    return 0;
}

/* fetch_tasks: 実行済みのstmt(上のResultRow7列構成のSELECT)から全件フェッチする。
 * 呼び出し側がmalloc配列を受け取り、各要素task_destroy+配列free()すること */
static TaskError fetch_tasks(MYSQL_STMT *stmt, Task ***out_tasks, size_t *out_count) {
    ResultRow row;
    memset(&row, 0, sizeof(row));
    MYSQL_BIND binds[7];
    bind_result_row(binds, &row);
    if (mysql_stmt_bind_result(stmt, binds) != 0) return TASK_ERR_DB_ERROR;
    if (mysql_stmt_store_result(stmt) != 0) return TASK_ERR_DB_ERROR;

    Task **tasks = NULL;
    size_t count = 0;
    while (mysql_stmt_fetch(stmt) == 0) {
        Task *t = task_create();
        if (t == NULL || row_to_task(&row, t) != 0) {
            task_destroy(t);
            for (size_t i = 0; i < count; i++) task_destroy(tasks[i]);
            free(tasks);
            return TASK_ERR_MEMORY_ERROR;
        }
        Task **grown = (Task **)realloc(tasks, sizeof(Task *) * (count + 1));
        if (grown == NULL) {
            task_destroy(t);
            for (size_t i = 0; i < count; i++) task_destroy(tasks[i]);
            free(tasks);
            return TASK_ERR_MEMORY_ERROR;
        }
        tasks = grown;
        tasks[count] = t;
        count += 1;
    }
    *out_tasks = tasks;
    *out_count = count;
    return TASK_OK;
}

/* attach_labels: N+1回避のため対象task_idをまとめて1回のIN句クエリで取得する
 *
 * 【メモリ管理のバッド/グッドプラクティス】countは呼び出し元のtask一覧件数に依存し
 * 上限が無いため、以下のような固定長スタックバッファ+strcatだと大量件数取得時に
 * バッファオーバーフローする(CWE-121、任意長データを固定長バッファに詰める典型的バグ):
 *
 *   char in_clause[256];  // ← 128件を超えるtask一覧を取得すると溢れる("?,"が2バイト×count)
 *   in_clause[0] = '\0';
 *   for (size_t i = 0; i < count; i++) strcat(in_clause, i == 0 ? "?" : ",?");
 *
 * 修正後: 必要バイト数(count * 2 + 1、"?"1個+",?"×(count-1)+NUL)を先に計算してから
 * mallocするため、countがいくつでも溢れない */
static TaskError attach_labels(MYSQL *conn, Task **tasks, size_t count) {
    if (count == 0) return TASK_OK;

    size_t in_clause_len = count * 2 + 1;
    char *in_clause = (char *)malloc(in_clause_len);
    if (in_clause == NULL) return TASK_ERR_MEMORY_ERROR;
    in_clause[0] = '\0';
    for (size_t i = 0; i < count; i++) {
        strcat(in_clause, i == 0 ? "?" : ",?");
    }

    char sql[512];
    snprintf(sql, sizeof(sql),
             "SELECT task_labels.task_id, labels.id, labels.name "
             "FROM task_labels JOIN labels ON labels.id = task_labels.label_id "
             "WHERE task_labels.task_id IN (%s)",
             in_clause);
    free(in_clause);

    MYSQL_STMT *stmt = mysql_stmt_init(conn);
    if (stmt == NULL) return TASK_ERR_DB_ERROR;
    if (mysql_stmt_prepare(stmt, sql, strlen(sql)) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }

    MYSQL_BIND *params = (MYSQL_BIND *)calloc(count, sizeof(MYSQL_BIND));
    int64_t *ids = (int64_t *)malloc(sizeof(int64_t) * count);
    if (params == NULL || ids == NULL) {
        free(params);
        free(ids);
        mysql_stmt_close(stmt);
        return TASK_ERR_MEMORY_ERROR;
    }
    for (size_t i = 0; i < count; i++) {
        ids[i] = tasks[i]->id;
        params[i].buffer_type = MYSQL_TYPE_LONGLONG;
        params[i].buffer = &ids[i];
    }
    if (mysql_stmt_bind_param(stmt, params) != 0 || mysql_stmt_execute(stmt) != 0) {
        free(params);
        free(ids);
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }

    int64_t task_id = 0;
    int64_t label_id = 0;
    char label_name[STR_BUF_LEN];
    unsigned long label_name_len = 0;
    MYSQL_BIND result_binds[3];
    memset(result_binds, 0, sizeof(result_binds));
    result_binds[0].buffer_type = MYSQL_TYPE_LONGLONG;
    result_binds[0].buffer = &task_id;
    result_binds[1].buffer_type = MYSQL_TYPE_LONGLONG;
    result_binds[1].buffer = &label_id;
    result_binds[2].buffer_type = MYSQL_TYPE_STRING;
    result_binds[2].buffer = label_name;
    result_binds[2].buffer_length = STR_BUF_LEN;
    result_binds[2].length = &label_name_len;

    mysql_stmt_bind_result(stmt, result_binds);
    mysql_stmt_store_result(stmt);
    while (mysql_stmt_fetch(stmt) == 0) {
        char name_buf[STR_BUF_LEN + 1];
        memcpy(name_buf, label_name, label_name_len);
        name_buf[label_name_len] = '\0';
        for (size_t i = 0; i < count; i++) {
            if (tasks[i]->id == task_id) {
                task_add_label(tasks[i], label_id, name_buf);
                break;
            }
        }
    }
    free(params);
    free(ids);
    mysql_stmt_close(stmt);
    return TASK_OK;
}

TaskError task_repository_list(int64_t user_id, int limit, int offset, Task ***out_tasks,
                                size_t *out_count, int64_t *out_total) {
    MYSQL *conn = mysql_conn_get();
    if (conn == NULL) return TASK_ERR_DB_ERROR;

    /* 件数取得 */
    {
        const char *count_sql = "SELECT COUNT(*) FROM tasks WHERE user_id = ?";
        MYSQL_STMT *count_stmt = mysql_stmt_init(conn);
        if (count_stmt == NULL) return TASK_ERR_DB_ERROR;
        if (mysql_stmt_prepare(count_stmt, count_sql, strlen(count_sql)) != 0) {
            mysql_stmt_close(count_stmt);
            return TASK_ERR_DB_ERROR;
        }
        MYSQL_BIND p;
        memset(&p, 0, sizeof(p));
        p.buffer_type = MYSQL_TYPE_LONGLONG;
        p.buffer = &user_id;
        mysql_stmt_bind_param(count_stmt, &p);
        mysql_stmt_execute(count_stmt);
        int64_t total = 0;
        MYSQL_BIND rb;
        memset(&rb, 0, sizeof(rb));
        rb.buffer_type = MYSQL_TYPE_LONGLONG;
        rb.buffer = &total;
        mysql_stmt_bind_result(count_stmt, &rb);
        mysql_stmt_store_result(count_stmt);
        mysql_stmt_fetch(count_stmt);
        mysql_stmt_close(count_stmt);
        *out_total = total;
    }

    const char *sql =
        "SELECT id, name, description, status, finished_on, created_at, updated_at "
        /* created_at同値(MySQL DATETIMEは秒精度のため、短時間に複数作成すると容易に同値になる)
         * だけでは並び順が不定になり、offsetページングで同じ行が2ページにまたがって
         * 出現したり、逆に1件も出現しないまま飛ばされたりし得る。idは1始まりのAUTO_INCREMENTで
         * 一意かつ挿入順を保つため、tie-breakとして追加する(外部公開API v1のページ境界を
         * またぐ結合テストで顕在化する、backend-rustのlist_tasks_offset_externalと同じ
         * ORDER BY規約に揃えた) */
        "FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?";
    MYSQL_STMT *stmt = mysql_stmt_init(conn);
    if (stmt == NULL) return TASK_ERR_DB_ERROR;
    if (mysql_stmt_prepare(stmt, sql, strlen(sql)) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }
    int64_t limit64 = limit;
    int64_t offset64 = offset;
    MYSQL_BIND params[3];
    memset(params, 0, sizeof(params));
    params[0].buffer_type = MYSQL_TYPE_LONGLONG;
    params[0].buffer = &user_id;
    params[1].buffer_type = MYSQL_TYPE_LONGLONG;
    params[1].buffer = &limit64;
    params[2].buffer_type = MYSQL_TYPE_LONGLONG;
    params[2].buffer = &offset64;
    mysql_stmt_bind_param(stmt, params);
    if (mysql_stmt_execute(stmt) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }

    TaskError err = fetch_tasks(stmt, out_tasks, out_count);
    mysql_stmt_close(stmt);
    if (err != TASK_OK) return err;
    return attach_labels(conn, *out_tasks, *out_count);
}

TaskError task_repository_list_cursor(int64_t user_id, int64_t after_id, int limit,
                                       Task ***out_tasks, size_t *out_count,
                                       int64_t *out_next_cursor) {
    MYSQL *conn = mysql_conn_get();
    if (conn == NULL) return TASK_ERR_DB_ERROR;

    const char *sql_with_cursor =
        "SELECT id, name, description, status, finished_on, created_at, updated_at "
        "FROM tasks WHERE user_id = ? AND id > ? ORDER BY id ASC LIMIT ?";
    const char *sql_from_start =
        "SELECT id, name, description, status, finished_on, created_at, updated_at "
        "FROM tasks WHERE user_id = ? ORDER BY id ASC LIMIT ?";
    int has_cursor = after_id > 0;
    const char *sql = has_cursor ? sql_with_cursor : sql_from_start;

    MYSQL_STMT *stmt = mysql_stmt_init(conn);
    if (stmt == NULL) return TASK_ERR_DB_ERROR;
    if (mysql_stmt_prepare(stmt, sql, strlen(sql)) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }

    int64_t limit64 = limit;
    MYSQL_BIND params[3];
    memset(params, 0, sizeof(params));
    params[0].buffer_type = MYSQL_TYPE_LONGLONG;
    params[0].buffer = &user_id;
    if (has_cursor) {
        params[1].buffer_type = MYSQL_TYPE_LONGLONG;
        params[1].buffer = &after_id;
        params[2].buffer_type = MYSQL_TYPE_LONGLONG;
        params[2].buffer = &limit64;
        mysql_stmt_bind_param(stmt, params);
    } else {
        params[1].buffer_type = MYSQL_TYPE_LONGLONG;
        params[1].buffer = &limit64;
        mysql_stmt_bind_param(stmt, params);
    }
    if (mysql_stmt_execute(stmt) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }

    TaskError err = fetch_tasks(stmt, out_tasks, out_count);
    mysql_stmt_close(stmt);
    if (err != TASK_OK) return err;

    err = attach_labels(conn, *out_tasks, *out_count);
    if (err != TASK_OK) return err;

    /* 【backend-rustと揃えた設計】取得件数がlimit未満なら「もう次ページは無い」と判断できる
     * (LIMIT句の結果が満杯でない=DB側にこれ以上該当行が無かったことを意味する)。
     * limitちょうど取れた場合のみ、次ページがあるかもしれないとみなし最後のidを返す
     * (backend-rust/src/grpc/task.rsのnext_cursor計算と同じヒューリスティック。
     * 総件数がlimitのちょうど倍数の場合に「空の次ページ」を1回余分に踏む可能性はあるが、
     * 他言語と同じ既知の簡略化として許容している) */
    *out_next_cursor =
        (*out_count > 0 && *out_count == (size_t)limit) ? (*out_tasks)[*out_count - 1]->id : 0;
    return TASK_OK;
}

TaskError task_repository_find_by_id(int64_t id, int64_t user_id, Task **out_task) {
    MYSQL *conn = mysql_conn_get();
    if (conn == NULL) return TASK_ERR_DB_ERROR;

    const char *sql =
        "SELECT id, name, description, status, finished_on, created_at, updated_at "
        "FROM tasks WHERE id = ? AND user_id = ?";
    MYSQL_STMT *stmt = mysql_stmt_init(conn);
    if (stmt == NULL) return TASK_ERR_DB_ERROR;
    if (mysql_stmt_prepare(stmt, sql, strlen(sql)) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }
    MYSQL_BIND params[2];
    memset(params, 0, sizeof(params));
    params[0].buffer_type = MYSQL_TYPE_LONGLONG;
    params[0].buffer = &id;
    params[1].buffer_type = MYSQL_TYPE_LONGLONG;
    params[1].buffer = &user_id;
    mysql_stmt_bind_param(stmt, params);
    if (mysql_stmt_execute(stmt) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }

    ResultRow row;
    memset(&row, 0, sizeof(row));
    MYSQL_BIND binds[7];
    bind_result_row(binds, &row);
    mysql_stmt_bind_result(stmt, binds);
    mysql_stmt_store_result(stmt);
    if (mysql_stmt_fetch(stmt) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_NOT_FOUND;
    }
    Task *t = task_create();
    if (t == NULL || row_to_task(&row, t) != 0) {
        task_destroy(t);
        mysql_stmt_close(stmt);
        return TASK_ERR_MEMORY_ERROR;
    }
    mysql_stmt_close(stmt);

    Task *tasks_arr[1] = {t};
    attach_labels(conn, tasks_arr, 1);
    *out_task = t;
    return TASK_OK;
}

/* replace_labels: 【既知バグの回帰防止】同一リクエスト内の重複label_id(例: [3,3,5])を
 * 重複排除してからINSERTする(CONTRACT.mdセクション23.1、複数言語で見つかった実バグと同種) */
static TaskError replace_labels(MYSQL *conn, int64_t task_id, const int64_t *label_ids,
                                 size_t label_id_count, const char *now) {
    {
        const char *del_sql = "DELETE FROM task_labels WHERE task_id = ?";
        MYSQL_STMT *del_stmt = mysql_stmt_init(conn);
        if (del_stmt == NULL) return TASK_ERR_DB_ERROR;
        if (mysql_stmt_prepare(del_stmt, del_sql, strlen(del_sql)) != 0) {
            mysql_stmt_close(del_stmt);
            return TASK_ERR_DB_ERROR;
        }
        MYSQL_BIND p;
        memset(&p, 0, sizeof(p));
        p.buffer_type = MYSQL_TYPE_LONGLONG;
        p.buffer = &task_id;
        mysql_stmt_bind_param(del_stmt, &p);
        int rc = mysql_stmt_execute(del_stmt);
        mysql_stmt_close(del_stmt);
        if (rc != 0) return TASK_ERR_DB_ERROR;
    }

    /* 重複排除: O(n^2)だが今回のTask比較アプリの規模(1リクエストあたり数個)では十分 */
    int64_t *seen = (int64_t *)malloc(sizeof(int64_t) * (label_id_count > 0 ? label_id_count : 1));
    size_t seen_count = 0;
    if (seen == NULL) return TASK_ERR_MEMORY_ERROR;

    for (size_t i = 0; i < label_id_count; i++) {
        int64_t label_id = label_ids[i];
        int already_seen = 0;
        for (size_t j = 0; j < seen_count; j++) {
            if (seen[j] == label_id) {
                already_seen = 1;
                break;
            }
        }
        if (already_seen) continue;
        seen[seen_count++] = label_id;

        const char *ins_sql =
            "INSERT INTO task_labels (task_id, label_id, created_at, updated_at) "
            "VALUES (?, ?, ?, ?)";
        MYSQL_STMT *ins_stmt = mysql_stmt_init(conn);
        if (ins_stmt == NULL) {
            free(seen);
            return TASK_ERR_DB_ERROR;
        }
        if (mysql_stmt_prepare(ins_stmt, ins_sql, strlen(ins_sql)) != 0) {
            mysql_stmt_close(ins_stmt);
            free(seen);
            return TASK_ERR_DB_ERROR;
        }
        MYSQL_BIND params[4];
        memset(params, 0, sizeof(params));
        params[0].buffer_type = MYSQL_TYPE_LONGLONG;
        params[0].buffer = &task_id;
        params[1].buffer_type = MYSQL_TYPE_LONGLONG;
        params[1].buffer = &label_id;
        unsigned long now_len = (unsigned long)strlen(now);
        params[2].buffer_type = MYSQL_TYPE_STRING;
        params[2].buffer = (void *)now;
        params[2].buffer_length = now_len;
        params[3].buffer_type = MYSQL_TYPE_STRING;
        params[3].buffer = (void *)now;
        params[3].buffer_length = now_len;
        mysql_stmt_bind_param(ins_stmt, params);
        int rc = mysql_stmt_execute(ins_stmt);
        mysql_stmt_close(ins_stmt);
        if (rc != 0) {
            free(seen);
            return TASK_ERR_DB_ERROR;
        }
    }
    free(seen);
    return TASK_OK;
}

TaskError task_repository_create(int64_t user_id, const TaskInput *input, int64_t *out_id) {
    MYSQL *conn = mysql_conn_get();
    if (conn == NULL) return TASK_ERR_DB_ERROR;
    if (mysql_autocommit(conn, 0) != 0) return TASK_ERR_DB_ERROR;

    char now[32];
    task_now_mysql_datetime(now, sizeof(now));

    TaskStatus status;
    task_status_from_string(input->status_raw, &status);

    const char *sql =
        "INSERT INTO tasks (name, description, status, finished_on, user_id, "
        "created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)";
    MYSQL_STMT *stmt = mysql_stmt_init(conn);
    if (stmt == NULL) {
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }
    if (mysql_stmt_prepare(stmt, sql, strlen(sql)) != 0) {
        mysql_stmt_close(stmt);
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }

    int64_t status_val = (int64_t)status;
    bool desc_is_null = input->description == NULL;
    unsigned long name_len = (unsigned long)strlen(input->name);
    unsigned long desc_len = input->description != NULL ? (unsigned long)strlen(input->description) : 0;
    unsigned long finished_on_len = (unsigned long)strlen(input->finished_on);
    unsigned long now_len = (unsigned long)strlen(now);

    MYSQL_BIND params[7];
    memset(params, 0, sizeof(params));
    params[0].buffer_type = MYSQL_TYPE_STRING;
    params[0].buffer = (void *)input->name;
    params[0].buffer_length = name_len;
    params[1].buffer_type = MYSQL_TYPE_STRING;
    params[1].buffer = (void *)(input->description != NULL ? input->description : "");
    params[1].buffer_length = desc_len;
    params[1].is_null = &desc_is_null;
    params[2].buffer_type = MYSQL_TYPE_LONGLONG;
    params[2].buffer = &status_val;
    params[3].buffer_type = MYSQL_TYPE_STRING;
    params[3].buffer = (void *)input->finished_on;
    params[3].buffer_length = finished_on_len;
    params[4].buffer_type = MYSQL_TYPE_LONGLONG;
    params[4].buffer = &user_id;
    params[5].buffer_type = MYSQL_TYPE_STRING;
    params[5].buffer = (void *)now;
    params[5].buffer_length = now_len;
    params[6].buffer_type = MYSQL_TYPE_STRING;
    params[6].buffer = (void *)now;
    params[6].buffer_length = now_len;
    mysql_stmt_bind_param(stmt, params);

    if (mysql_stmt_execute(stmt) != 0) {
        mysql_stmt_close(stmt);
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }
    int64_t task_id = (int64_t)mysql_stmt_insert_id(stmt);
    mysql_stmt_close(stmt);

    TaskError label_err =
        replace_labels(conn, task_id, input->label_ids, input->label_id_count, now);
    if (label_err != TASK_OK) {
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return label_err;
    }

    mysql_commit(conn);
    mysql_autocommit(conn, 1);
    *out_id = task_id;
    return TASK_OK;
}

TaskError task_repository_update(int64_t id, int64_t user_id, const TaskInput *input) {
    MYSQL *conn = mysql_conn_get();
    if (conn == NULL) return TASK_ERR_DB_ERROR;
    if (mysql_autocommit(conn, 0) != 0) return TASK_ERR_DB_ERROR;

    char now[32];
    task_now_mysql_datetime(now, sizeof(now));
    TaskStatus status;
    task_status_from_string(input->status_raw, &status);

    const char *sql =
        "UPDATE tasks SET name = ?, description = ?, status = ?, finished_on = ?, "
        "updated_at = ? WHERE id = ? AND user_id = ?";
    MYSQL_STMT *stmt = mysql_stmt_init(conn);
    if (stmt == NULL) {
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }
    if (mysql_stmt_prepare(stmt, sql, strlen(sql)) != 0) {
        mysql_stmt_close(stmt);
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }

    int64_t status_val = (int64_t)status;
    bool desc_is_null = input->description == NULL;
    unsigned long name_len = (unsigned long)strlen(input->name);
    unsigned long desc_len = input->description != NULL ? (unsigned long)strlen(input->description) : 0;
    unsigned long finished_on_len = (unsigned long)strlen(input->finished_on);
    unsigned long now_len = (unsigned long)strlen(now);

    MYSQL_BIND params[7];
    memset(params, 0, sizeof(params));
    params[0].buffer_type = MYSQL_TYPE_STRING;
    params[0].buffer = (void *)input->name;
    params[0].buffer_length = name_len;
    params[1].buffer_type = MYSQL_TYPE_STRING;
    params[1].buffer = (void *)(input->description != NULL ? input->description : "");
    params[1].buffer_length = desc_len;
    params[1].is_null = &desc_is_null;
    params[2].buffer_type = MYSQL_TYPE_LONGLONG;
    params[2].buffer = &status_val;
    params[3].buffer_type = MYSQL_TYPE_STRING;
    params[3].buffer = (void *)input->finished_on;
    params[3].buffer_length = finished_on_len;
    params[4].buffer_type = MYSQL_TYPE_STRING;
    params[4].buffer = (void *)now;
    params[4].buffer_length = now_len;
    params[5].buffer_type = MYSQL_TYPE_LONGLONG;
    params[5].buffer = &id;
    params[6].buffer_type = MYSQL_TYPE_LONGLONG;
    params[6].buffer = &user_id;
    mysql_stmt_bind_param(stmt, params);

    if (mysql_stmt_execute(stmt) != 0) {
        mysql_stmt_close(stmt);
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }
    my_ulonglong affected = mysql_stmt_affected_rows(stmt);
    mysql_stmt_close(stmt);
    if (affected == 0) {
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_NOT_FOUND;
    }

    TaskError label_err = replace_labels(conn, id, input->label_ids, input->label_id_count, now);
    if (label_err != TASK_OK) {
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return label_err;
    }

    mysql_commit(conn);
    mysql_autocommit(conn, 1);
    return TASK_OK;
}

TaskError task_repository_delete(int64_t id, int64_t user_id) {
    MYSQL *conn = mysql_conn_get();
    if (conn == NULL) return TASK_ERR_DB_ERROR;
    if (mysql_autocommit(conn, 0) != 0) return TASK_ERR_DB_ERROR;

    const char *del_task_sql = "DELETE FROM tasks WHERE id = ? AND user_id = ?";
    MYSQL_STMT *stmt = mysql_stmt_init(conn);
    if (stmt == NULL) {
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }
    if (mysql_stmt_prepare(stmt, del_task_sql, strlen(del_task_sql)) != 0) {
        mysql_stmt_close(stmt);
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }
    MYSQL_BIND params[2];
    memset(params, 0, sizeof(params));
    params[0].buffer_type = MYSQL_TYPE_LONGLONG;
    params[0].buffer = &id;
    params[1].buffer_type = MYSQL_TYPE_LONGLONG;
    params[1].buffer = &user_id;
    mysql_stmt_bind_param(stmt, params);
    if (mysql_stmt_execute(stmt) != 0) {
        mysql_stmt_close(stmt);
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }
    my_ulonglong affected = mysql_stmt_affected_rows(stmt);
    mysql_stmt_close(stmt);
    if (affected == 0) {
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_NOT_FOUND;
    }

    /* 【メモリ管理ではなくデータ整合性のバッド/グッドプラクティス】
     * task_labelsに外部キー制約は無いため、以下のような「素朴な実装」だと
     * tasks側のDELETE成功後・task_labels側のDELETE実行前にプロセスクラッシュや
     * DB接続断が起きると孤立行(task_idの参照先が存在しないtask_labels行)が残る
     * (backend-rustで実際に発生していた既知バグと同種、README.md「削除のトランザクション保護」参照):
     *
     *   mysql_autocommit(conn, 1);  // ← 各文が個別に即コミットされてしまう
     *   mysql_query(conn, "DELETE FROM tasks WHERE id = ...");
     *   mysql_query(conn, "DELETE FROM task_labels WHERE task_id = ...");  // ← ここで失敗/中断すると孤立行が残る
     *
     * 修正後: mysql_autocommit(conn, 0)で自動コミットを止め、両方のDELETEが両方とも
     * 成功した場合のみmysql_commit(conn)する(どちらかが失敗したらmysql_rollback(conn)で
     * tasks側のDELETEも取り消す、関数冒頭のmysql_autocommit(conn, 0)参照) */
    const char *del_labels_sql = "DELETE FROM task_labels WHERE task_id = ?";
    MYSQL_STMT *label_stmt = mysql_stmt_init(conn);
    if (label_stmt == NULL) {
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }
    if (mysql_stmt_prepare(label_stmt, del_labels_sql, strlen(del_labels_sql)) != 0) {
        mysql_stmt_close(label_stmt);
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }
    MYSQL_BIND lp;
    memset(&lp, 0, sizeof(lp));
    lp.buffer_type = MYSQL_TYPE_LONGLONG;
    lp.buffer = &id;
    mysql_stmt_bind_param(label_stmt, &lp);
    int rc = mysql_stmt_execute(label_stmt);
    mysql_stmt_close(label_stmt);
    if (rc != 0) {
        mysql_rollback(conn);
        mysql_autocommit(conn, 1);
        return TASK_ERR_DB_ERROR;
    }

    mysql_commit(conn);
    mysql_autocommit(conn, 1);
    return TASK_OK;
}

TaskError task_repository_find_user_by_id(int64_t id, bool *out_found) {
    MYSQL *conn = mysql_conn_get();
    if (conn == NULL) return TASK_ERR_DB_ERROR;

    const char *sql = "SELECT id FROM users WHERE id = ?";
    MYSQL_STMT *stmt = mysql_stmt_init(conn);
    if (stmt == NULL) return TASK_ERR_DB_ERROR;
    if (mysql_stmt_prepare(stmt, sql, strlen(sql)) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }
    MYSQL_BIND param;
    memset(&param, 0, sizeof(param));
    param.buffer_type = MYSQL_TYPE_LONGLONG;
    param.buffer = &id;
    mysql_stmt_bind_param(stmt, &param);
    if (mysql_stmt_execute(stmt) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }

    int64_t found_id = 0;
    MYSQL_BIND result;
    memset(&result, 0, sizeof(result));
    result.buffer_type = MYSQL_TYPE_LONGLONG;
    result.buffer = &found_id;
    mysql_stmt_bind_result(stmt, &result);
    mysql_stmt_store_result(stmt);
    *out_found = (mysql_stmt_fetch(stmt) == 0);
    mysql_stmt_close(stmt);
    return TASK_OK;
}

TaskError task_repository_find_user_by_keycloak_sub(const char *keycloak_sub, int64_t *out_id,
                                                     bool *out_found) {
    MYSQL *conn = mysql_conn_get();
    if (conn == NULL) return TASK_ERR_DB_ERROR;

    const char *sql =
        "SELECT users.id FROM users "
        "JOIN user_keycloaks ON user_keycloaks.user_id = users.id "
        "WHERE user_keycloaks.keycloak_sub = ?";
    MYSQL_STMT *stmt = mysql_stmt_init(conn);
    if (stmt == NULL) return TASK_ERR_DB_ERROR;
    if (mysql_stmt_prepare(stmt, sql, strlen(sql)) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }
    unsigned long sub_len = (unsigned long)strlen(keycloak_sub);
    MYSQL_BIND param;
    memset(&param, 0, sizeof(param));
    param.buffer_type = MYSQL_TYPE_STRING;
    param.buffer = (void *)keycloak_sub;
    param.buffer_length = sub_len;
    mysql_stmt_bind_param(stmt, &param);
    if (mysql_stmt_execute(stmt) != 0) {
        mysql_stmt_close(stmt);
        return TASK_ERR_DB_ERROR;
    }

    int64_t found_id = 0;
    MYSQL_BIND result;
    memset(&result, 0, sizeof(result));
    result.buffer_type = MYSQL_TYPE_LONGLONG;
    result.buffer = &found_id;
    mysql_stmt_bind_result(stmt, &result);
    mysql_stmt_store_result(stmt);
    *out_found = (mysql_stmt_fetch(stmt) == 0);
    mysql_stmt_close(stmt);
    if (*out_found) *out_id = found_id;
    return TASK_OK;
}
