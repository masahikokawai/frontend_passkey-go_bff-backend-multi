#include "grpc/task_mapper.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

/*
 * MySQL DATETIME文字列("YYYY-MM-DD HH:MM:SS"、常にUTCとして保存されている、
 * CONTRACT.mdセクション23.1のUTC統一方針)をprotobufのTimestampへ変換する
 * (backend-cppのMysqlDatetimeToTimestampと同じ扱い: naiveな日時をUTCとして解釈する)。
 * 失敗時(malloc失敗)はNULLを返す
 */
static Google__Protobuf__Timestamp *mysql_datetime_to_timestamp(const char *mysql_dt) {
    Google__Protobuf__Timestamp *ts =
        (Google__Protobuf__Timestamp *)malloc(sizeof(Google__Protobuf__Timestamp));
    if (ts == NULL) return NULL;
    google__protobuf__timestamp__init(ts);

    struct tm tm;
    memset(&tm, 0, sizeof(tm));
    sscanf(mysql_dt, "%d-%d-%d %d:%d:%d", &tm.tm_year, &tm.tm_mon, &tm.tm_mday, &tm.tm_hour,
           &tm.tm_min, &tm.tm_sec);
    tm.tm_year -= 1900;
    tm.tm_mon -= 1;
    /* timegmはローカルタイムゾーンを一切見ず、tmをUTCとして解釈してepoch秒に変換する
     * (POSIX拡張、mktime+タイムゾーン計算のようなローカルタイムゾーン依存を避けるため採用) */
    ts->seconds = (int64_t)timegm(&tm);
    ts->nanos = 0;
    return ts;
}

Task__V1__Task *task_mapper_to_proto(const Task *task) {
    Task__V1__Task *pb = (Task__V1__Task *)malloc(sizeof(Task__V1__Task));
    if (pb == NULL) return NULL;
    task__v1__task__init(pb);

    pb->id = (uint64_t)task->id;
    pb->name = strdup(task->name != NULL ? task->name : "");
    /*
     * 【protobuf-cの制約への対応(CMakeLists.txt「protobuf-cの既知の制約」節参照)】
     * optionalを外した結果、descriptionはhas_descriptionを持たない通常フィールドになった。
     * そのためtask->descriptionがNULL(未設定)の場合と空文字列の場合を区別して
     * ワイヤーに乗せることはできず、両方とも空文字列として送る(このプロジェクトでは
     * 実害の無い簡略化として許容している) */
    pb->description = strdup(task->description != NULL ? task->description : "");
    pb->status = strdup(task_status_to_string(task->status));
    pb->finished_on = strdup(task->finished_on);
    if (pb->name == NULL || pb->description == NULL || pb->status == NULL ||
        pb->finished_on == NULL) {
        task__v1__task__free_unpacked(pb, NULL);
        return NULL;
    }

    if (task->label_count > 0) {
        pb->labels = (Task__V1__Label **)calloc(task->label_count, sizeof(Task__V1__Label *));
        if (pb->labels == NULL) {
            task__v1__task__free_unpacked(pb, NULL);
            return NULL;
        }
        for (size_t i = 0; i < task->label_count; i++) {
            Task__V1__Label *label = (Task__V1__Label *)malloc(sizeof(Task__V1__Label));
            if (label == NULL) {
                /* n_labelsをここまでに完成した件数に合わせてからfree_unpackedする
                 * (未初期化のlabels[i]をfree_unpackedに辿らせないため) */
                pb->n_labels = i;
                task__v1__task__free_unpacked(pb, NULL);
                return NULL;
            }
            task__v1__label__init(label);
            label->id = (uint64_t)task->labels[i].id;
            label->name = strdup(task->labels[i].name != NULL ? task->labels[i].name : "");
            if (label->name == NULL) {
                free(label);
                pb->n_labels = i;
                task__v1__task__free_unpacked(pb, NULL);
                return NULL;
            }
            pb->labels[i] = label;
        }
        pb->n_labels = task->label_count;
    }

    pb->created_at = mysql_datetime_to_timestamp(task->created_at);
    pb->updated_at = mysql_datetime_to_timestamp(task->updated_at);
    if (pb->created_at == NULL || pb->updated_at == NULL) {
        task__v1__task__free_unpacked(pb, NULL);
        return NULL;
    }

    return pb;
}
