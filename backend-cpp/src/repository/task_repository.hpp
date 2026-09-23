#pragma once

#include <optional>
#include <string>

#include "common/error.hpp"
#include "db/connection_pool.hpp"
#include "domain/task.hpp"

namespace backend_cpp::repository {

// SQLはRepository層にのみ書く(Handler/Serviceから生SQLを叩かない、ORM禁止の方針、
// README.md参照)。全メソッドはブロッキングであることを前提とする
// (呼び出し側がdb::RunBlockingでdb_thread_poolへディスパッチすること)
class TaskRepository {
 public:
  explicit TaskRepository(db::ConnectionPool& pool) : pool_(pool) {}

  common::Result<std::vector<domain::Task>> ListByUser(int64_t user_id, int limit, int offset,
                                                         int64_t& total_out);
  common::Result<domain::Task> FindById(int64_t id, int64_t user_id);
  common::Result<int64_t> Create(int64_t user_id, const domain::TaskInput& input,
                                  domain::TaskStatus status);
  // 戻り値: 更新できた場合true、対象行が(他人のtaskも含め)見つからない場合false
  common::Result<bool> Update(int64_t id, int64_t user_id, const domain::TaskInput& input,
                               domain::TaskStatus status);
  // tasksとtask_labelsの両方の削除を1つのトランザクションで包む
  // (直前にRustで見つけて修正したのと同じ設計、README.md参照)
  common::Result<bool> Delete(int64_t id, int64_t user_id);

  // JWT認証(auth/jwt.hpp)のuser_id解決用。ローカル発行issuerはsubがそのままusers.id、
  // Keycloak発行issuerはsub=keycloak_subをuser_keycloaksテーブル経由で引く
  // (backend(Go)のresolve_user_id、backend-rustのsrc/auth/mod.rsと同じ設計)
  std::optional<int64_t> FindUserById(int64_t id);
  std::optional<int64_t> FindUserIdByKeycloakSub(const std::string& keycloak_sub);

  // 外部公開API(CONTRACT.mdセクション11)向け。内部CRUDのListByUserとは別メソッドに
  // 分離している(offset/cursor切り替えの主題を内部CRUDと混ぜないため、backend-rustと同じ方針)
  common::Result<std::vector<domain::Task>> ListOffsetExternal(int64_t user_id, int page,
                                                                 int page_size, int64_t& total_out);
  // id昇順のkeyset pagination。next_cursorは最後に返したtaskのid(無ければnullopt)
  common::Result<std::vector<domain::Task>> ListCursorExternal(int64_t user_id,
                                                                  std::optional<int64_t> after_id,
                                                                  int limit,
                                                                  std::optional<int64_t>& next_cursor_out);

 private:
  db::ConnectionPool& pool_;
};

}  // namespace backend_cpp::repository
