#ifndef REPOSITORY_TASK_REPOSITORY_H
#define REPOSITORY_TASK_REPOSITORY_H

#include <stdbool.h>

#include "common/error.h"
#include "domain/task.h"

/*
 * out_tasks: 成功時、呼び出し側はmalloc配列(Task*の配列)を受け取る
 * 各要素はtask_destroy()、配列自体はfree()すること
 */
TaskError task_repository_list(int64_t user_id, int limit, int offset, Task ***out_tasks,
                                size_t *out_count, int64_t *out_total);

/*
 * id昇順のkeyset pagination(gRPC v2向け。REST v1のoffset方式とは別に用意する、
 * backend-cppのListCursorExternalと同じ簡略設計。このプロジェクトの学習用途では
 * idのみのシンプルなカーソルで十分としている)。after_id<=0のときは先頭から取得する
 * (idは1始まりのAUTO_INCREMENTのため0以下は「未指定」を表せる)。
 * out_next_cursor: 0なら次ページ無し、それ以外なら次回のafter_idとして渡す値
 */
TaskError task_repository_list_cursor(int64_t user_id, int64_t after_id, int limit,
                                       Task ***out_tasks, size_t *out_count,
                                       int64_t *out_next_cursor);

/* out_task: TASK_OKのとき、呼び出し側はtask_destroy()すること */
TaskError task_repository_find_by_id(int64_t id, int64_t user_id, Task **out_task);

TaskError task_repository_create(int64_t user_id, const TaskInput *input, int64_t *out_id);

/* 対象行が(他人のtaskも含め)見つからない場合はTASK_ERR_NOT_FOUNDを返す */
TaskError task_repository_update(int64_t id, int64_t user_id, const TaskInput *input);

/*
 * tasksとtask_labelsの両方の削除を1つのMySQLトランザクションで包む
 * (mysql_autocommit(conn, 0)→両方のDELETE→mysql_commit(conn)、失敗時はmysql_rollback(conn))
 * backend-cpp/backend-rust(修正後)と同じ設計。task_labelsに外部キー制約は無いため、
 * トランザクション無しだと孤立行が残り得る(backend-rustで見つかった既知バグと同じ問題)
 */
TaskError task_repository_delete(int64_t id, int64_t user_id);

/*
 * JWT認証(auth/jwt.h・auth/user_resolver.h)のuser_id解決用。ローカル(HMAC/RSA)発行の
 * JWTはsubが内部user_idそのものであるため、存在確認のみ行う(idが実際にusers行として
 * 存在するかどうか)。*out_foundがfalseの場合、認証は「user not provisioned」として拒否する
 */
TaskError task_repository_find_user_by_id(int64_t id, bool *out_found);

/*
 * Keycloak発行のJWTはsub=keycloak_subであり、これはusersテーブルではなく
 * user_keycloaksテーブルに分離されている(migration 000008_split_user_credentials参照)ため、
 * JOIN経由でusers.idを引く
 */
TaskError task_repository_find_user_by_keycloak_sub(const char *keycloak_sub, int64_t *out_id,
                                                     bool *out_found);

#endif /* REPOSITORY_TASK_REPOSITORY_H */
