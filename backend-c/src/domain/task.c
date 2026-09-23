#include "domain/task.h"

#include <stdlib.h>
#include <string.h>

const char *task_status_to_string(TaskStatus status) {
    switch (status) {
        case TASK_STATUS_WAITING:
            return "waiting";
        case TASK_STATUS_WORK_IN_PROGRESS:
            return "work_in_progress";
        case TASK_STATUS_COMPLETED:
            return "completed";
        default:
            return "waiting";
    }
}

int task_status_from_string(const char *s, TaskStatus *out) {
    if (s == NULL) return -1;
    if (strcmp(s, "waiting") == 0) {
        *out = TASK_STATUS_WAITING;
        return 0;
    }
    if (strcmp(s, "work_in_progress") == 0) {
        *out = TASK_STATUS_WORK_IN_PROGRESS;
        return 0;
    }
    if (strcmp(s, "completed") == 0) {
        *out = TASK_STATUS_COMPLETED;
        return 0;
    }
    return -1;
}

Task *task_create(void) {
    Task *t = (Task *)calloc(1, sizeof(Task));
    return t; /* callocが失敗すればNULLを返す、呼び出し側でチェックすること */
}

void task_destroy(Task *task) {
    if (task == NULL) return;
    free(task->name);
    free(task->description);
    for (size_t i = 0; i < task->label_count; i++) {
        free(task->labels[i].name);
    }
    free(task->labels);
    free(task);
}

/*
 * 【メモリ管理のバッド/グッドプラクティス】nameは長さの上限が無い(バリデーションは
 * この関数より上位のtask_validate_inputで行うため、ここでは任意長を安全に扱う必要がある)。
 * 以下のような固定長バッファ+strcpyだと、nameが長い場合にバッファオーバーフローする
 * (CWE-120、固定長宛先へ可変長入力を無検査でコピーする典型的バグ):
 *
 *   char buf[64];
 *   strcpy(buf, name);  // ← nameが64バイト以上ならoverflow(呼び出し元のスタックを破壊しうる)
 *   task->name = buf;   // ← さらにローカル配列のアドレスを保持してしまう二重のバグ(スコープを抜けると解放済み領域を指す)
 *
 * 修正後: strdup()でヒープに必要なだけ複製する(上限なし)。加えて、先にstrdup()して
 * 成功を確認してから既存値をfree()する順序にしている点にも意味がある。逆順
 * (先にfree(task->name)してからstrdup()する)だと、strdup()がNULLを返した場合に
 * task->nameが解放済みの不正なポインタ(dangling pointer)のまま残ってしまう
 */
int task_set_name(Task *task, const char *name) {
    char *copy = name != NULL ? strdup(name) : strdup("");
    if (copy == NULL) return -1;
    free(task->name);
    task->name = copy;
    return 0;
}

int task_set_description(Task *task, const char *description) {
    if (description == NULL) {
        free(task->description);
        task->description = NULL;
        return 0;
    }
    char *copy = strdup(description);
    if (copy == NULL) return -1;
    free(task->description);
    task->description = copy;
    return 0;
}

int task_add_label(Task *task, int64_t label_id, const char *label_name) {
    char *name_copy = strdup(label_name != NULL ? label_name : "");
    if (name_copy == NULL) return -1;

    Label *grown = (Label *)realloc(task->labels, sizeof(Label) * (task->label_count + 1));
    if (grown == NULL) {
        free(name_copy);
        return -1;
    }
    task->labels = grown;
    task->labels[task->label_count].id = label_id;
    task->labels[task->label_count].name = name_copy;
    task->label_count += 1;
    return 0;
}

TaskInput *task_input_create(void) {
    return (TaskInput *)calloc(1, sizeof(TaskInput));
}

void task_input_destroy(TaskInput *input) {
    if (input == NULL) return;
    free(input->name);
    free(input->description);
    free(input->label_ids);
    free(input);
}

int task_input_set_name(TaskInput *input, const char *name) {
    char *copy = strdup(name != NULL ? name : "");
    if (copy == NULL) return -1;
    free(input->name);
    input->name = copy;
    return 0;
}

int task_input_set_description(TaskInput *input, const char *description) {
    if (description == NULL) {
        free(input->description);
        input->description = NULL;
        return 0;
    }
    char *copy = strdup(description);
    if (copy == NULL) return -1;
    free(input->description);
    input->description = copy;
    return 0;
}

int task_input_add_label_id(TaskInput *input, int64_t label_id) {
    int64_t *grown =
        (int64_t *)realloc(input->label_ids, sizeof(int64_t) * (input->label_id_count + 1));
    if (grown == NULL) return -1;
    input->label_ids = grown;
    input->label_ids[input->label_id_count] = label_id;
    input->label_id_count += 1;
    return 0;
}
