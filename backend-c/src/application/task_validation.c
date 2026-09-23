#include "application/task_validation.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "common/time.h"

TaskError task_parse_input(const cJSON *json, TaskInput **out_input) {
    if (json == NULL || !cJSON_IsObject(json)) return TASK_ERR_INVALID_REQUEST;

    TaskInput *input = task_input_create();
    if (input == NULL) return TASK_ERR_MEMORY_ERROR;

    const cJSON *name = cJSON_GetObjectItemCaseSensitive(json, "name");
    const cJSON *status = cJSON_GetObjectItemCaseSensitive(json, "status");
    const cJSON *finished_on = cJSON_GetObjectItemCaseSensitive(json, "finished_on");

    const char *name_str = cJSON_IsString(name) ? name->valuestring : "";
    const char *status_str = cJSON_IsString(status) ? status->valuestring : "";
    const char *finished_on_str = cJSON_IsString(finished_on) ? finished_on->valuestring : "";

    if (name_str[0] == '\0' || status_str[0] == '\0' || finished_on_str[0] == '\0') {
        task_input_destroy(input);
        return TASK_ERR_INVALID_REQUEST;
    }
    if (!task_is_valid_iso_date(finished_on_str)) {
        task_input_destroy(input);
        return TASK_ERR_INVALID_FINISHED_ON;
    }

    if (task_input_set_name(input, name_str) != 0) {
        task_input_destroy(input);
        return TASK_ERR_MEMORY_ERROR;
    }
    snprintf(input->status_raw, sizeof(input->status_raw), "%s", status_str);
    snprintf(input->finished_on, sizeof(input->finished_on), "%s", finished_on_str);

    const cJSON *description = cJSON_GetObjectItemCaseSensitive(json, "description");
    if (description != NULL && cJSON_IsString(description)) {
        if (task_input_set_description(input, description->valuestring) != 0) {
            task_input_destroy(input);
            return TASK_ERR_MEMORY_ERROR;
        }
    }

    const cJSON *label_ids = cJSON_GetObjectItemCaseSensitive(json, "label_ids");
    if (label_ids != NULL && cJSON_IsArray(label_ids)) {
        const cJSON *elem = NULL;
        cJSON_ArrayForEach(elem, label_ids) {
            if (cJSON_IsNumber(elem)) {
                if (task_input_add_label_id(input, (int64_t)elem->valuedouble) != 0) {
                    task_input_destroy(input);
                    return TASK_ERR_MEMORY_ERROR;
                }
            }
        }
    }

    *out_input = input;
    return TASK_OK;
}

TaskError task_validate_input(const TaskInput *input, TaskStatus *out_status,
                               char **out_message) {
    *out_message = NULL;

    if (task_utf8_codepoint_length(input->name) > 20) {
        *out_message = strdup("nameは20文字以内である必要があります");
        return TASK_ERR_VALIDATION_ERROR;
    }

    char today[11];
    task_today_utc_iso(today, sizeof(today));
    if (strcmp(input->finished_on, today) < 0) {
        *out_message = strdup("finished_onに過去日は指定できません");
        return TASK_ERR_VALIDATION_ERROR;
    }

    TaskStatus status;
    if (task_status_from_string(input->status_raw, &status) != 0) {
        char buf[64];
        snprintf(buf, sizeof(buf), "不明なstatus: \"%s\"", input->status_raw);
        *out_message = strdup(buf);
        return TASK_ERR_VALIDATION_ERROR;
    }

    *out_status = status;
    return TASK_OK;
}
