#ifndef HTTP_TASK_JSON_H
#define HTTP_TASK_JSON_H

#include <cjson/cJSON.h>

#include "domain/task.h"

/*
 * 内部REST v1(src/http/handler.c)・外部公開API(src/external/external_handler.c)の
 * 両方で共有するTask→JSON変換。CONTRACT.mdセクション5.1のJSON形状(user_idを含まない)
 * は元々内部REST v1向けに決めたものだが、外部公開APIが要求する形状(CONTRACT.mdセクション11
 * 「レスポンス」、こちらもuser_idを含まない)とたまたま完全に一致するため、
 * 2つ目の変換関数を新設せずこの1つを両トランスポートで再利用する
 * (backend-cppのTaskToJsonが内部用・外部用で別々の実装になっているのとは異なる、
 * backend-c独自の簡略化)
 */
cJSON *task_to_json(const Task *t);

#endif /* HTTP_TASK_JSON_H */
