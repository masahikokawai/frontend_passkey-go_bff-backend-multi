#include "http/task_json.h"

#include <stdio.h>
#include <string.h>

/* backend-cpp/src/application/task_handler.cppのTaskToJsonと1文字も変えていない
 * (CONTRACT.mdセクション5.1、キー順序はJSONとしては意味を持たないがcurl比較の便宜上揃える) */
cJSON *task_to_json(const Task *t) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddNumberToObject(root, "id", (double)t->id);
    cJSON_AddStringToObject(root, "name", t->name != NULL ? t->name : "");
    if (t->description != NULL) {
        cJSON_AddStringToObject(root, "description", t->description);
    } else {
        cJSON_AddNullToObject(root, "description");
    }
    cJSON_AddStringToObject(root, "status", task_status_to_string(t->status));
    cJSON_AddStringToObject(root, "finished_on", t->finished_on);

    cJSON *labels = cJSON_AddArrayToObject(root, "labels");
    for (size_t i = 0; i < t->label_count; i++) {
        cJSON *label = cJSON_CreateObject();
        cJSON_AddNumberToObject(label, "id", (double)t->labels[i].id);
        cJSON_AddStringToObject(label, "name", t->labels[i].name);
        cJSON_AddItemToArray(labels, label);
    }

    /* "YYYY-MM-DD HH:MM:SS" -> "YYYY-MM-DDTHH:MM:SS+00:00" */
    char created_at[40];
    char updated_at[40];
    snprintf(created_at, sizeof(created_at), "%s", t->created_at);
    snprintf(updated_at, sizeof(updated_at), "%s", t->updated_at);
    if (strlen(created_at) > 10) created_at[10] = 'T';
    if (strlen(updated_at) > 10) updated_at[10] = 'T';
    strncat(created_at, "+00:00", sizeof(created_at) - strlen(created_at) - 1);
    strncat(updated_at, "+00:00", sizeof(updated_at) - strlen(updated_at) - 1);
    cJSON_AddStringToObject(root, "created_at", created_at);
    cJSON_AddStringToObject(root, "updated_at", updated_at);
    return root;
}
