#include "grpc/task_grpc_handlers.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "application/task_validation.h"
#include "common/time.h"
#include "domain/task.h"
#include "grpc/task_mapper.h"
#include "repository/task_repository.h"

/* protobuf-cリクエストのname/description/status/finished_on/label_idsからTaskInputを
 * 組み立てる(REST側のtask_parse_inputに相当。JSONではなくprotoから読む点だけが違う)。
 * 失敗時(malloc失敗)はNULLを返す */
static TaskInput *build_task_input(const char *name, const char *description,
                                    const char *status, const char *finished_on,
                                    size_t n_label_ids, const uint64_t *label_ids) {
    TaskInput *input = task_input_create();
    if (input == NULL) return NULL;

    if (task_input_set_name(input, name != NULL ? name : "") != 0) {
        task_input_destroy(input);
        return NULL;
    }
    /* descriptionはprotobuf-cのoptional非対応の都合で空文字列と未設定を区別できない
     * (task_mapper.c参照)。ここでは空文字列をそのままdescriptionとして扱う(NULL化しない) */
    if (task_input_set_description(input, description != NULL ? description : "") != 0) {
        task_input_destroy(input);
        return NULL;
    }
    snprintf(input->status_raw, sizeof(input->status_raw), "%s", status != NULL ? status : "");
    snprintf(input->finished_on, sizeof(input->finished_on), "%s",
              finished_on != NULL ? finished_on : "");

    for (size_t i = 0; i < n_label_ids; i++) {
        if (task_input_add_label_id(input, (int64_t)label_ids[i]) != 0) {
            task_input_destroy(input);
            return NULL;
        }
    }
    return input;
}

TaskError grpc_handle_list_tasks(int64_t user_id, const Task__V1__ListTasksRequest *req,
                                  Task__V1__ListTasksResponse **out_response) {
    /* 【このフェーズの既知の制約】name/status/label_idsによる絞り込みは未対応
     * (REST v1側も同じ簡略化、backend-cppのListTasksと同じ、README.md参照)。
     * idのみのkeyset cursorで一覧する */
    int limit = req->limit <= 0 ? 20 : req->limit;
    int64_t after_id = (int64_t)req->cursor;

    Task **tasks = NULL;
    size_t count = 0;
    int64_t next_cursor = 0;
    TaskError err = task_repository_list_cursor(user_id, after_id, limit, &tasks, &count,
                                                 &next_cursor);
    if (err != TASK_OK) return err;

    Task__V1__ListTasksResponse *response =
        (Task__V1__ListTasksResponse *)malloc(sizeof(Task__V1__ListTasksResponse));
    if (response == NULL) {
        for (size_t i = 0; i < count; i++) task_destroy(tasks[i]);
        free(tasks);
        return TASK_ERR_MEMORY_ERROR;
    }
    task__v1__list_tasks_response__init(response);
    response->next_cursor = (uint64_t)next_cursor;

    if (count > 0) {
        response->tasks = (Task__V1__Task **)calloc(count, sizeof(Task__V1__Task *));
        if (response->tasks == NULL) {
            free(response);
            for (size_t i = 0; i < count; i++) task_destroy(tasks[i]);
            free(tasks);
            return TASK_ERR_MEMORY_ERROR;
        }
        for (size_t i = 0; i < count; i++) {
            Task__V1__Task *pb = task_mapper_to_proto(tasks[i]);
            if (pb == NULL) {
                response->n_tasks = i;
                task__v1__list_tasks_response__free_unpacked(response, NULL);
                for (size_t j = i; j < count; j++) task_destroy(tasks[j]);
                free(tasks);
                return TASK_ERR_MEMORY_ERROR;
            }
            response->tasks[i] = pb;
        }
        response->n_tasks = count;
    }

    for (size_t i = 0; i < count; i++) task_destroy(tasks[i]);
    free(tasks);

    *out_response = response;
    return TASK_OK;
}

TaskError grpc_handle_get_task(int64_t user_id, const Task__V1__GetTaskRequest *req,
                                Task__V1__Task **out_task) {
    Task *task = NULL;
    TaskError err = task_repository_find_by_id((int64_t)req->id, user_id, &task);
    if (err != TASK_OK) return err;

    Task__V1__Task *pb = task_mapper_to_proto(task);
    task_destroy(task);
    if (pb == NULL) return TASK_ERR_MEMORY_ERROR;

    *out_task = pb;
    return TASK_OK;
}

TaskError grpc_handle_create_task(int64_t user_id, const Task__V1__CreateTaskRequest *req,
                                   Task__V1__Task **out_task, char **out_message) {
    if (!task_is_valid_iso_date(req->finished_on)) return TASK_ERR_INVALID_FINISHED_ON;

    TaskInput *input = build_task_input(req->name, req->description, req->status,
                                         req->finished_on, req->n_label_ids, req->label_ids);
    if (input == NULL) return TASK_ERR_MEMORY_ERROR;

    TaskStatus status;
    TaskError err = task_validate_input(input, &status, out_message);
    if (err != TASK_OK) {
        task_input_destroy(input);
        return err;
    }

    int64_t new_id = 0;
    err = task_repository_create(user_id, input, &new_id);
    task_input_destroy(input);
    if (err != TASK_OK) return err;

    Task *task = NULL;
    err = task_repository_find_by_id(new_id, user_id, &task);
    if (err != TASK_OK) return err;

    Task__V1__Task *pb = task_mapper_to_proto(task);
    task_destroy(task);
    if (pb == NULL) return TASK_ERR_MEMORY_ERROR;

    *out_task = pb;
    return TASK_OK;
}

TaskError grpc_handle_update_task(int64_t user_id, const Task__V1__UpdateTaskRequest *req,
                                   Task__V1__Task **out_task, char **out_message) {
    if (!task_is_valid_iso_date(req->finished_on)) return TASK_ERR_INVALID_FINISHED_ON;

    TaskInput *input = build_task_input(req->name, req->description, req->status,
                                         req->finished_on, req->n_label_ids, req->label_ids);
    if (input == NULL) return TASK_ERR_MEMORY_ERROR;

    TaskStatus status;
    TaskError err = task_validate_input(input, &status, out_message);
    if (err != TASK_OK) {
        task_input_destroy(input);
        return err;
    }

    int64_t id = (int64_t)req->id;
    err = task_repository_update(id, user_id, input);
    task_input_destroy(input);
    if (err != TASK_OK) return err;

    Task *task = NULL;
    err = task_repository_find_by_id(id, user_id, &task);
    if (err != TASK_OK) return err;

    Task__V1__Task *pb = task_mapper_to_proto(task);
    task_destroy(task);
    if (pb == NULL) return TASK_ERR_MEMORY_ERROR;

    *out_task = pb;
    return TASK_OK;
}

TaskError grpc_handle_delete_task(int64_t user_id, const Task__V1__DeleteTaskRequest *req) {
    /* tasksとtask_labelsの削除は1つのトランザクションで包まれている
     * (task_repository_delete、README.md「削除のトランザクション保護」節参照) */
    return task_repository_delete((int64_t)req->id, user_id);
}
