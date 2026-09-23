#include "repository/task_repository.hpp"

#include <mysql/mysql.h>

#include <array>
#include <chrono>
#include <deque>
#include <cstdio>
#include <cstring>
#include <ctime>
#include <optional>
#include <set>
#include <stdexcept>
#include <vector>

#include "common/time.hpp"

namespace backend_cpp::repository {

using backend_cpp::common::AppError;
using backend_cpp::common::DbError;
using backend_cpp::common::NotFound;
using backend_cpp::common::Result;
using backend_cpp::domain::Label;
using backend_cpp::domain::StatusFromString;
using backend_cpp::domain::StatusToString;
using backend_cpp::domain::Task;
using backend_cpp::domain::TaskInput;
using backend_cpp::domain::TaskStatus;

namespace {

// MYSQL_STMT(prepared statement)を薄くRAIIで包む
// (Handler/Serviceからは見えないRepository内部限定のヘルパー)
class Statement {
 public:
  Statement(MYSQL* conn, const std::string& sql) : conn_(conn) {
    stmt_ = mysql_stmt_init(conn);
    if (!stmt_) throw std::runtime_error("mysql_stmt_init failed");
    if (mysql_stmt_prepare(stmt_, sql.c_str(), sql.size()) != 0) {
      std::string err = mysql_stmt_error(stmt_);
      mysql_stmt_close(stmt_);
      throw std::runtime_error("mysql_stmt_prepare failed: " + err);
    }
  }
  ~Statement() {
    if (stmt_) mysql_stmt_close(stmt_);
  }
  Statement(const Statement&) = delete;
  MYSQL_STMT* raw() { return stmt_; }

 private:
  MYSQL* conn_;
  MYSQL_STMT* stmt_;
};

// 可変長引数のパラメータバインド用の小さなビルダー(int64/文字列/NULLのみサポート、
// このリポジトリで必要な範囲に限定している)
struct ParamBuilder {
  std::vector<MYSQL_BIND> binds;
  std::vector<int64_t> int_storage;
  std::vector<std::string> str_storage;
  // std::vector<bool>はビット特殊化されておりアドレスが取れないため、
  // 要素アドレスが安定するdequeを使う(&is_null_storage.back()を後で取るため)
  std::deque<bool> is_null_storage;
  std::vector<unsigned long> len_storage;

  // 文字列を追加する前にstr_storageの再確保でポインタが壊れないよう、事前に個数を確保しておく
  void Reserve(size_t n) {
    int_storage.reserve(n);
    str_storage.reserve(n);
    // is_null_storageはdequeなのでreserve不要(要素アドレスは常に安定)
    len_storage.reserve(n);
  }

  void AddInt64(int64_t v) {
    int_storage.push_back(v);
    MYSQL_BIND b{};
    b.buffer_type = MYSQL_TYPE_LONGLONG;
    b.buffer = &int_storage.back();
    binds.push_back(b);
  }
  void AddString(const std::string& v) {
    str_storage.push_back(v);
    len_storage.push_back(static_cast<unsigned long>(str_storage.back().size()));
    MYSQL_BIND b{};
    b.buffer_type = MYSQL_TYPE_STRING;
    b.buffer = str_storage.back().data();
    b.buffer_length = static_cast<unsigned long>(str_storage.back().size());
    b.length = &len_storage.back();
    binds.push_back(b);
  }
  void AddStringOrNull(const std::optional<std::string>& v) {
    if (!v.has_value()) {
      is_null_storage.push_back(1);
      MYSQL_BIND b{};
      b.buffer_type = MYSQL_TYPE_NULL;
      b.is_null = &is_null_storage.back();
      binds.push_back(b);
      return;
    }
    AddString(*v);
  }
};

void BindParams(Statement& stmt, ParamBuilder& params) {
  if (!params.binds.empty()) {
    if (mysql_stmt_bind_param(stmt.raw(), params.binds.data()) != 0) {
      throw std::runtime_error(std::string("mysql_stmt_bind_param failed: ") +
                                mysql_stmt_error(stmt.raw()));
    }
  }
}

void Execute(Statement& stmt) {
  if (mysql_stmt_execute(stmt.raw()) != 0) {
    throw std::runtime_error(std::string("mysql_stmt_execute failed: ") +
                              mysql_stmt_error(stmt.raw()));
  }
}

constexpr size_t kStrBufLen = 1024;

// 結果カラム用の固定長文字列バッファ(id等の数値はint64、文字列/日時は固定長バッファで受ける)
struct ResultRow {
  int64_t id = 0;
  char name[kStrBufLen]{};
  unsigned long name_len = 0;
  char description[kStrBufLen]{};
  unsigned long description_len = 0;
  bool description_is_null = false;
  int64_t status = 0;
  char finished_on[16]{};
  unsigned long finished_on_len = 0;
  char created_at[32]{};
  unsigned long created_at_len = 0;
  char updated_at[32]{};
  unsigned long updated_at_len = 0;

  std::array<MYSQL_BIND, 6> Binds() {
    std::array<MYSQL_BIND, 6> b{};
    b[0].buffer_type = MYSQL_TYPE_LONGLONG;
    b[0].buffer = &id;
    b[1].buffer_type = MYSQL_TYPE_STRING;
    b[1].buffer = name;
    b[1].buffer_length = kStrBufLen;
    b[1].length = &name_len;
    b[2].buffer_type = MYSQL_TYPE_STRING;
    b[2].buffer = description;
    b[2].buffer_length = kStrBufLen;
    b[2].length = &description_len;
    b[2].is_null = &description_is_null;
    b[3].buffer_type = MYSQL_TYPE_LONGLONG;
    b[3].buffer = &status;
    b[4].buffer_type = MYSQL_TYPE_STRING;
    b[4].buffer = finished_on;
    b[4].buffer_length = sizeof(finished_on);
    b[4].length = &finished_on_len;
    b[5].buffer_type = MYSQL_TYPE_STRING;
    // NOTE: created_at/updated_atは別配列にまとめて追加する(下記ToTaskで組み立てる)
    return b;
  }
};

Task RowToTask(const ResultRow& row) {
  Task t;
  t.id = row.id;
  t.name.assign(row.name, row.name_len);
  if (!row.description_is_null) {
    t.description = std::string(row.description, row.description_len);
  }
  t.status = static_cast<TaskStatus>(row.status);
  t.finished_on.assign(row.finished_on, row.finished_on_len);
  t.created_at.assign(row.created_at, row.created_at_len);
  t.updated_at.assign(row.updated_at, row.updated_at_len);
  return t;
}

// Statement実行済み(SELECT id,name,description,status,finished_on,created_at,updated_at
// の7列固定)からTaskの配列を全件フェッチする。ListByUser/ListOffsetExternal/
// ListCursorExternalで共用する
std::vector<Task> FetchTasks(Statement& stmt) {
  ResultRow row;
  auto binds = row.Binds();
  binds[5].buffer = row.created_at;
  binds[5].buffer_length = sizeof(row.created_at);
  binds[5].length = &row.created_at_len;
  std::array<MYSQL_BIND, 7> full{};
  for (size_t i = 0; i < 6; ++i) full[i] = binds[i];
  full[6].buffer_type = MYSQL_TYPE_STRING;
  full[6].buffer = row.updated_at;
  full[6].buffer_length = sizeof(row.updated_at);
  full[6].length = &row.updated_at_len;

  mysql_stmt_bind_result(stmt.raw(), full.data());
  mysql_stmt_store_result(stmt.raw());
  std::vector<Task> tasks;
  while (mysql_stmt_fetch(stmt.raw()) == 0) tasks.push_back(RowToTask(row));
  return tasks;
}

// ラベルを付与する(N+1回避のため対象task_idをまとめて1回で取得)。
// ListByUser/ListOffsetExternal/ListCursorExternalで共用する
void AttachLabels(MYSQL* conn, std::vector<Task>& tasks) {
  if (tasks.empty()) return;
  std::string in_clause;
  for (size_t i = 0; i < tasks.size(); ++i) in_clause += (i ? ",?" : "?");
  Statement label_stmt(conn, "SELECT task_labels.task_id, labels.id, labels.name "
                              "FROM task_labels JOIN labels ON labels.id = task_labels.label_id "
                              "WHERE task_labels.task_id IN (" +
                                  in_clause + ")");
  ParamBuilder lp;
  lp.Reserve(tasks.size());
  for (auto& t : tasks) lp.AddInt64(t.id);
  BindParams(label_stmt, lp);
  Execute(label_stmt);

  int64_t task_id = 0;
  int64_t label_id = 0;
  char label_name[kStrBufLen]{};
  unsigned long label_name_len = 0;
  std::array<MYSQL_BIND, 3> lb{};
  lb[0].buffer_type = MYSQL_TYPE_LONGLONG;
  lb[0].buffer = &task_id;
  lb[1].buffer_type = MYSQL_TYPE_LONGLONG;
  lb[1].buffer = &label_id;
  lb[2].buffer_type = MYSQL_TYPE_STRING;
  lb[2].buffer = label_name;
  lb[2].buffer_length = kStrBufLen;
  lb[2].length = &label_name_len;
  mysql_stmt_bind_result(label_stmt.raw(), lb.data());
  mysql_stmt_store_result(label_stmt.raw());
  while (mysql_stmt_fetch(label_stmt.raw()) == 0) {
    for (auto& t : tasks) {
      if (t.id == task_id) {
        t.labels.push_back(Label{label_id, std::string(label_name, label_name_len)});
        break;
      }
    }
  }
}

}  // namespace

Result<std::vector<Task>> TaskRepository::ListByUser(int64_t user_id, int limit, int offset,
                                                       int64_t& total_out) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  try {
    {
      Statement count_stmt(conn, "SELECT COUNT(*) FROM tasks WHERE user_id = ?");
      ParamBuilder p;
      p.Reserve(1);
      p.AddInt64(user_id);
      BindParams(count_stmt, p);
      Execute(count_stmt);
      int64_t total = 0;
      MYSQL_BIND rb{};
      rb.buffer_type = MYSQL_TYPE_LONGLONG;
      rb.buffer = &total;
      mysql_stmt_bind_result(count_stmt.raw(), &rb);
      mysql_stmt_store_result(count_stmt.raw());
      mysql_stmt_fetch(count_stmt.raw());
      total_out = total;
    }

    Statement stmt(conn,
                    "SELECT id, name, description, status, finished_on, created_at, updated_at "
                    "FROM tasks WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?");
    ParamBuilder p;
    p.Reserve(3);
    p.AddInt64(user_id);
    p.AddInt64(limit);
    p.AddInt64(offset);
    BindParams(stmt, p);
    Execute(stmt);

    ResultRow row;
    auto binds = row.Binds();
    binds[5].buffer = row.created_at;
    binds[5].buffer_length = sizeof(row.created_at);
    binds[5].length = &row.created_at_len;
    std::array<MYSQL_BIND, 7> full{};
    for (size_t i = 0; i < 6; ++i) full[i] = binds[i];
    full[6].buffer_type = MYSQL_TYPE_STRING;
    full[6].buffer = row.updated_at;
    full[6].buffer_length = sizeof(row.updated_at);
    full[6].length = &row.updated_at_len;

    mysql_stmt_bind_result(stmt.raw(), full.data());
    mysql_stmt_store_result(stmt.raw());

    std::vector<Task> tasks;
    while (mysql_stmt_fetch(stmt.raw()) == 0) {
      tasks.push_back(RowToTask(row));
    }

    AttachLabels(conn, tasks);
    return tasks;
  } catch (const std::exception&) {
    return std::unexpected(DbError());
  }
}

// CONTRACT.mdセクション11・backend/internal/repository/task.goのListOffsetForExternalAPIに
// 対応。「created_at DESC, id DESC」で安定ソートする(内部CRUDのListByUserとはソートが異なる)
Result<std::vector<Task>> TaskRepository::ListOffsetExternal(int64_t user_id, int page,
                                                               int page_size,
                                                               int64_t& total_out) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  try {
    {
      Statement count_stmt(conn, "SELECT COUNT(*) FROM tasks WHERE user_id = ?");
      ParamBuilder p;
      p.Reserve(1);
      p.AddInt64(user_id);
      BindParams(count_stmt, p);
      Execute(count_stmt);
      int64_t total = 0;
      MYSQL_BIND rb{};
      rb.buffer_type = MYSQL_TYPE_LONGLONG;
      rb.buffer = &total;
      mysql_stmt_bind_result(count_stmt.raw(), &rb);
      mysql_stmt_store_result(count_stmt.raw());
      mysql_stmt_fetch(count_stmt.raw());
      total_out = total;
    }

    int64_t offset = static_cast<int64_t>(page - 1) * page_size;
    Statement stmt(conn,
                    "SELECT id, name, description, status, finished_on, created_at, updated_at "
                    "FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?");
    ParamBuilder p;
    p.Reserve(3);
    p.AddInt64(user_id);
    p.AddInt64(page_size);
    p.AddInt64(offset);
    BindParams(stmt, p);
    Execute(stmt);

    auto tasks = FetchTasks(stmt);
    AttachLabels(conn, tasks);
    return tasks;
  } catch (const std::exception&) {
    return std::unexpected(DbError());
  }
}

// id昇順のkeyset pagination(このプロジェクトの学習用の簡略版。backend-rustはcreated_at+id
// の複合カーソルだが、ここではidのみのシンプルなカーソルにしている)
Result<std::vector<Task>> TaskRepository::ListCursorExternal(int64_t user_id,
                                                               std::optional<int64_t> after_id,
                                                               int limit,
                                                               std::optional<int64_t>& next_cursor_out) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  try {
    std::string sql =
        "SELECT id, name, description, status, finished_on, created_at, updated_at "
        "FROM tasks WHERE user_id = ?";
    if (after_id.has_value()) sql += " AND id > ?";
    sql += " ORDER BY id ASC LIMIT ?";

    Statement stmt(conn, sql);
    ParamBuilder p;
    p.Reserve(3);
    p.AddInt64(user_id);
    if (after_id.has_value()) p.AddInt64(*after_id);
    p.AddInt64(limit);
    BindParams(stmt, p);
    Execute(stmt);

    auto tasks = FetchTasks(stmt);
    AttachLabels(conn, tasks);
    next_cursor_out = tasks.empty() ? std::nullopt : std::optional<int64_t>(tasks.back().id);
    return tasks;
  } catch (const std::exception&) {
    return std::unexpected(DbError());
  }
}

Result<Task> TaskRepository::FindById(int64_t id, int64_t user_id) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  try {
    Statement stmt(conn,
                    "SELECT id, name, description, status, finished_on, created_at, updated_at "
                    "FROM tasks WHERE id = ? AND user_id = ?");
    ParamBuilder p;
    p.Reserve(2);
    p.AddInt64(id);
    p.AddInt64(user_id);
    BindParams(stmt, p);
    Execute(stmt);

    ResultRow row;
    auto binds = row.Binds();
    std::array<MYSQL_BIND, 7> full{};
    for (size_t i = 0; i < 6; ++i) full[i] = binds[i];
    full[6].buffer_type = MYSQL_TYPE_STRING;
    full[6].buffer = row.updated_at;
    full[6].buffer_length = sizeof(row.updated_at);
    full[6].length = &row.updated_at_len;
    full[5].buffer = row.created_at;
    full[5].buffer_length = sizeof(row.created_at);
    full[5].length = &row.created_at_len;

    mysql_stmt_bind_result(stmt.raw(), full.data());
    mysql_stmt_store_result(stmt.raw());
    if (mysql_stmt_fetch(stmt.raw()) != 0) {
      return std::unexpected(NotFound());
    }
    Task t = RowToTask(row);

    Statement label_stmt(conn,
                          "SELECT labels.id, labels.name FROM task_labels "
                          "JOIN labels ON labels.id = task_labels.label_id WHERE task_labels.task_id = ?");
    ParamBuilder lp;
    lp.Reserve(1);
    lp.AddInt64(id);
    BindParams(label_stmt, lp);
    Execute(label_stmt);
    int64_t label_id = 0;
    char label_name[kStrBufLen]{};
    unsigned long label_name_len = 0;
    std::array<MYSQL_BIND, 2> lb{};
    lb[0].buffer_type = MYSQL_TYPE_LONGLONG;
    lb[0].buffer = &label_id;
    lb[1].buffer_type = MYSQL_TYPE_STRING;
    lb[1].buffer = label_name;
    lb[1].buffer_length = kStrBufLen;
    lb[1].length = &label_name_len;
    mysql_stmt_bind_result(label_stmt.raw(), lb.data());
    mysql_stmt_store_result(label_stmt.raw());
    while (mysql_stmt_fetch(label_stmt.raw()) == 0) {
      t.labels.push_back(Label{label_id, std::string(label_name, label_name_len)});
    }
    return t;
  } catch (const std::exception&) {
    return std::unexpected(DbError());
  }
}

namespace {
// 【既知バグの回帰防止】同一リクエスト内の重複label_id(例: [3,3,5])を重複排除してから
// INSERTする(CONTRACT.mdセクション23.1、5言語中4言語で見つかった実バグと同種)
void ReplaceLabels(MYSQL* conn, int64_t task_id, const std::vector<int64_t>& label_ids,
                    const std::string& now) {
  {
    Statement del(conn, "DELETE FROM task_labels WHERE task_id = ?");
    ParamBuilder p;
    p.Reserve(1);
    p.AddInt64(task_id);
    BindParams(del, p);
    Execute(del);
  }
  std::set<int64_t> seen;
  for (int64_t label_id : label_ids) {
    if (seen.contains(label_id)) continue;
    seen.insert(label_id);
    Statement ins(conn,
                  "INSERT INTO task_labels (task_id, label_id, created_at, updated_at) "
                  "VALUES (?, ?, ?, ?)");
    ParamBuilder p;
    p.Reserve(4);
    p.AddInt64(task_id);
    p.AddInt64(label_id);
    p.AddString(now);
    p.AddString(now);
    BindParams(ins, p);
    Execute(ins);
  }
}

std::string NowMysqlDatetime() {
  // finished_on/created_at/updated_atはUTC基準で統一する(README.md・common/time.hpp参照)
  const auto now = std::chrono::system_clock::now();
  const std::time_t tt = std::chrono::system_clock::to_time_t(now);
  std::tm utc_tm{};
  gmtime_r(&tt, &utc_tm);
  char buf[20];
  std::snprintf(buf, sizeof(buf), "%04d-%02d-%02d %02d:%02d:%02d", utc_tm.tm_year + 1900,
                utc_tm.tm_mon + 1, utc_tm.tm_mday, utc_tm.tm_hour, utc_tm.tm_min, utc_tm.tm_sec);
  return std::string(buf);
}
}  // namespace

Result<int64_t> TaskRepository::Create(int64_t user_id, const TaskInput& input,
                                        TaskStatus status) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  if (mysql_autocommit(conn, 0) != 0) return std::unexpected(DbError());
  try {
    const std::string now = NowMysqlDatetime();
    Statement stmt(conn,
                    "INSERT INTO tasks (name, description, status, finished_on, user_id, "
                    "created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)");
    ParamBuilder p;
    p.Reserve(7);
    p.AddString(input.name);
    p.AddStringOrNull(input.description);
    p.AddInt64(static_cast<int64_t>(status));
    p.AddString(input.finished_on);
    p.AddInt64(user_id);
    p.AddString(now);
    p.AddString(now);
    BindParams(stmt, p);
    Execute(stmt);
    int64_t task_id = static_cast<int64_t>(mysql_stmt_insert_id(stmt.raw()));

    ReplaceLabels(conn, task_id, input.label_ids, now);
    mysql_commit(conn);
    mysql_autocommit(conn, 1);
    return task_id;
  } catch (const std::exception&) {
    mysql_rollback(conn);
    mysql_autocommit(conn, 1);
    return std::unexpected(DbError());
  }
}

Result<bool> TaskRepository::Update(int64_t id, int64_t user_id, const TaskInput& input,
                                     TaskStatus status) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  if (mysql_autocommit(conn, 0) != 0) return std::unexpected(DbError());
  try {
    const std::string now = NowMysqlDatetime();
    Statement stmt(conn,
                    "UPDATE tasks SET name = ?, description = ?, status = ?, finished_on = ?, "
                    "updated_at = ? WHERE id = ? AND user_id = ?");
    ParamBuilder p;
    p.Reserve(7);
    p.AddString(input.name);
    p.AddStringOrNull(input.description);
    p.AddInt64(static_cast<int64_t>(status));
    p.AddString(input.finished_on);
    p.AddString(now);
    p.AddInt64(id);
    p.AddInt64(user_id);
    BindParams(stmt, p);
    Execute(stmt);
    if (mysql_stmt_affected_rows(stmt.raw()) == 0) {
      mysql_rollback(conn);
      mysql_autocommit(conn, 1);
      return false;
    }
    ReplaceLabels(conn, id, input.label_ids, now);
    mysql_commit(conn);
    mysql_autocommit(conn, 1);
    return true;
  } catch (const std::exception&) {
    mysql_rollback(conn);
    mysql_autocommit(conn, 1);
    return std::unexpected(DbError());
  }
}

Result<bool> TaskRepository::Delete(int64_t id, int64_t user_id) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  if (mysql_autocommit(conn, 0) != 0) return std::unexpected(DbError());
  try {
    // tasksとtask_labelsの両方の削除を1つのトランザクションで包む
    // (task_labelsに外部キー制約は無く、トランザクション無しだと孤立行が残り得る。
    // backend-rustで見つかった既知バグと同じ問題、README.md参照)
    Statement del_task(conn, "DELETE FROM tasks WHERE id = ? AND user_id = ?");
    ParamBuilder p;
    p.Reserve(2);
    p.AddInt64(id);
    p.AddInt64(user_id);
    BindParams(del_task, p);
    Execute(del_task);
    if (mysql_stmt_affected_rows(del_task.raw()) == 0) {
      mysql_rollback(conn);
      mysql_autocommit(conn, 1);
      return false;
    }

    Statement del_labels(conn, "DELETE FROM task_labels WHERE task_id = ?");
    ParamBuilder lp;
    lp.Reserve(1);
    lp.AddInt64(id);
    BindParams(del_labels, lp);
    Execute(del_labels);

    mysql_commit(conn);
    mysql_autocommit(conn, 1);
    return true;
  } catch (const std::exception&) {
    mysql_rollback(conn);
    mysql_autocommit(conn, 1);
    return std::unexpected(DbError());
  }
}

std::optional<int64_t> TaskRepository::FindUserById(int64_t id) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  try {
    Statement stmt(conn, "SELECT id FROM users WHERE id = ?");
    ParamBuilder p;
    p.Reserve(1);
    p.AddInt64(id);
    BindParams(stmt, p);
    Execute(stmt);
    int64_t out_id = 0;
    MYSQL_BIND bind{};
    bind.buffer_type = MYSQL_TYPE_LONGLONG;
    bind.buffer = &out_id;
    mysql_stmt_bind_result(stmt.raw(), &bind);
    if (mysql_stmt_fetch(stmt.raw()) != 0) return std::nullopt;
    return out_id;
  } catch (const std::exception&) {
    return std::nullopt;
  }
}

std::optional<int64_t> TaskRepository::FindUserIdByKeycloakSub(const std::string& keycloak_sub) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  try {
    Statement stmt(conn,
                    "SELECT users.id FROM users "
                    "JOIN user_keycloaks ON user_keycloaks.user_id = users.id "
                    "WHERE user_keycloaks.keycloak_sub = ?");
    ParamBuilder p;
    p.Reserve(1);
    p.AddString(keycloak_sub);
    BindParams(stmt, p);
    Execute(stmt);
    int64_t out_id = 0;
    MYSQL_BIND bind{};
    bind.buffer_type = MYSQL_TYPE_LONGLONG;
    bind.buffer = &out_id;
    mysql_stmt_bind_result(stmt.raw(), &bind);
    if (mysql_stmt_fetch(stmt.raw()) != 0) return std::nullopt;
    return out_id;
  } catch (const std::exception&) {
    return std::nullopt;
  }
}

}  // namespace backend_cpp::repository
