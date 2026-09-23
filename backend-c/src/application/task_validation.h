#ifndef APPLICATION_TASK_VALIDATION_H
#define APPLICATION_TASK_VALIDATION_H

#include <cjson/cJSON.h>

#include "common/error.h"
#include "domain/task.h"

/*
 * backend-cpp/src/application/task_handler.cppのParseTaskInputと同じ規則
 * (name/status/finished_onは空文字列も必須違反として扱う、backend(Go)のbinding:"required"と
 * 同じ挙動)。json自体がオブジェクトでない/必須フィールド欠如ならTASK_ERR_INVALID_REQUEST、
 * finished_onがYYYY-MM-DD形式として不正ならTASK_ERR_INVALID_FINISHED_ONを返す
 * out_input: TASK_OK時のみ設定される。呼び出し側はtask_input_destroy()すること
 */
TaskError task_parse_input(const cJSON *json, TaskInput **out_input);

/*
 * backend-cpp/src/application/task_handler.cppのValidateTaskInputと同じ規則:
 *   - nameは20 UTF-8コードポイント以内
 *   - finished_onは(UTC基準の)今日より過去であってはならない
 *   - statusはwaiting/work_in_progress/completedのいずれか
 * いずれの違反もTASK_ERR_VALIDATION_ERRORを返し、out_messageにmalloc済みメッセージを
 * 設定する(呼び出し側がfree()すること)。成功時はout_statusにパース済みステータスを設定する
 */
TaskError task_validate_input(const TaskInput *input, TaskStatus *out_status, char **out_message);

#endif /* APPLICATION_TASK_VALIDATION_H */
