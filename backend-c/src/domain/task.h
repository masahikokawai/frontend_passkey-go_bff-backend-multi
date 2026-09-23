#ifndef DOMAIN_TASK_H
#define DOMAIN_TASK_H

#include <stddef.h>
#include <stdint.h>

/* backend/internal/model/enum.go の TaskStatus と同じ数値対応
 * (waiting=1/work_in_progress=2/completed=3、backend-cpp/src/domain/task.hppと同一) */
typedef enum {
    TASK_STATUS_WAITING = 1,
    TASK_STATUS_WORK_IN_PROGRESS = 2,
    TASK_STATUS_COMPLETED = 3,
} TaskStatus;

const char *task_status_to_string(TaskStatus status);
/* 戻り値: 成功時0、不明なstatus文字列なら-1(*outは変更しない) */
int task_status_from_string(const char *s, TaskStatus *out);

typedef struct {
    int64_t id;
    char *name; /* 所有(malloc)。labelはtask_add_labelでコピーされる */
} Label;

/*
 * Cならではの所有権規約:
 *   - task_create() で確保したTaskは、必ずtask_destroy()で解放する(create/destroyペア)
 *   - name/description/labels配列はTask自身が所有し、task_destroy()がまとめて解放する
 *   - C++版(backend-cpp)はこの所有権をスマートポインタ/std::string/std::vectorが
 *     デストラクタで自動的に処理するが、Cでは全て手動で対応する必要がある
 */
typedef struct {
    int64_t id;
    char *name;        /* 所有(malloc)、NUL終端 */
    char *description; /* 所有(malloc)、NULL可(未設定) */
    TaskStatus status;
    char finished_on[11]; /* "YYYY-MM-DD" + NUL */
    Label *labels;         /* 所有(malloc配列)、各要素のnameも所有 */
    size_t label_count;
    char created_at[32]; /* "YYYY-MM-DD HH:MM:SS" (MySQL DATETIME文字列のまま保持) */
    char updated_at[32];
} Task;

/* task_create: ゼロ初期化されたTask*を返す。呼び出し側は必ずtask_destroy()すること */
Task *task_create(void);
/* task_destroy: name/description/labels(各labelのnameを含む)を解放してからTask自体を解放する
 * NULLを渡しても安全(何もしない) */
void task_destroy(Task *task);

/* task_set_name/task_set_description: 既存の値を解放してから複製をセットする
 * 戻り値: 成功時0、malloc失敗時-1(この場合フィールドは変更されない) */
int task_set_name(Task *task, const char *name);
int task_set_description(Task *task, const char *description); /* NULLでクリア */

/* task_add_label: labels配列をrealloc拡張し、nameを複製して追加する
 * 戻り値: 成功時0、malloc/realloc失敗時-1 */
int task_add_label(Task *task, int64_t label_id, const char *label_name);

/* リクエストボディから作る入力型(バリデーション前)。所有権規約はTaskと同じ */
typedef struct {
    char *name;             /* 所有、NULL可(未設定=空文字列相当) */
    char *description;      /* 所有、NULL可 */
    char status_raw[32];
    char finished_on[11];
    int64_t *label_ids;      /* 所有(malloc配列) */
    size_t label_id_count;
} TaskInput;

TaskInput *task_input_create(void);
void task_input_destroy(TaskInput *input);
int task_input_set_name(TaskInput *input, const char *name);
int task_input_set_description(TaskInput *input, const char *description);
/* task_input_add_label_id: label_ids配列をrealloc拡張する。戻り値: 成功時0、失敗時-1 */
int task_input_add_label_id(TaskInput *input, int64_t label_id);

#endif /* DOMAIN_TASK_H */
