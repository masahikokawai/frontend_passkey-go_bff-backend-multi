#ifndef GRPC_TASK_MAPPER_H
#define GRPC_TASK_MAPPER_H

#include "domain/task.h"
#include "task/v1/task.pb-c.h"

/*
 * domain::Task(Repositoryが返すドメインモデル)をTask__V1__Task(protobuf-cメッセージ)へ
 * 変換する。backend-cppのToProto(grpc/task_mapper.cpp)と同じ役割で、gRPCハンドラが
 * protoの都合をドメインへ漏らさないようにする(README.md「gRPC」節参照)。
 *
 * 戻り値はmalloc木構造(name/description/status/finished_on文字列・Label配列・
 * created_at/updated_atのTimestampを含む)であり、NULL以外が返った場合は
 * 呼び出し側がtask__v1__task__free_unpacked(result, NULL)で解放すること。
 * 【3層のメモリモデルの2層目】protobuf-cのfree_unpackedはunpack()で得たメッセージ用の
 * 解放関数だが、ディスクリプタを辿って各フィールドをallocator->freeで再帰的に解放するだけの
 * 実装のため、ここでmalloc/strdup(=システムのfree()と対応する)で手動構築した木にも
 * そのまま使える(protobuf-c公式のサーバー側パターン)。失敗時(malloc失敗)はNULLを返す
 */
Task__V1__Task *task_mapper_to_proto(const Task *task);

#endif /* GRPC_TASK_MAPPER_H */
