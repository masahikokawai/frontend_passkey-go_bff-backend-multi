'use strict';

const mysql = require('mysql2/promise');
const { URL } = require('node:url');
const { TaskStatus, statusFromDb } = require('./model');
const { nowMysqlDatetime } = require('./time');

function buildPoolConfig(dsn) {
  const url = new URL(dsn);
  return {
    host: url.hostname,
    port: url.port ? parseInt(url.port, 10) : 3306,
    user: url.username ? decodeURIComponent(url.username) : 'root',
    password: url.password ? decodeURIComponent(url.password) : '',
    database: url.pathname.replace(/^\//, ''),
    // MySQLのDATE/DATETIMEを常にUTC基準の文字列のまま扱う(Dateオブジェクトへ変換すると
    // Node実行環境のローカルタイムゾーンが介在してしまい、過去に発見された
    // 「finished_onの過去日判定がタイムゾーンでずれる」バグ(CONTRACT.mdセクション23.1)と
    // 同種の問題を再発しかねないため、文字列のまま扱うことで最初から回避する)
    dateStrings: true,
    waitForConnections: true,
    connectionLimit: 10,
  };
}

function connect(dsn) {
  return mysql.createPool(buildPoolConfig(dsn));
}

// 【backend(Go)実装との突き合わせで判明した点】usersテーブルはkeycloak_subカラムを
// 持たない(migration 000008でuser_keycloaksテーブルへ分離済み、CONTRACT.mdセクション16.2)
async function findUserById(pool, id) {
  const [rows] = await pool.execute('SELECT id, email, name FROM users WHERE id = ?', [id]);
  return rows[0] ? rowToUser(rows[0]) : null;
}

async function findUserByKeycloakSub(pool, sub) {
  const [rows] = await pool.execute(
    `SELECT users.id AS id, users.email AS email, users.name AS name
     FROM users JOIN user_keycloaks ON user_keycloaks.user_id = users.id
     WHERE user_keycloaks.keycloak_sub = ?`,
    [sub],
  );
  return rows[0] ? rowToUser(rows[0]) : null;
}

function rowToUser(row) {
  return { id: row.id, email: row.email, name: row.name };
}

// name/status/label_idsの絞り込み条件をWHERE句へ組み立てる
// (backend(Go)のrepository.Task.scopedと同じ条件: nameはLIKE部分一致、statusは完全一致、
// label_idsは「いずれかのラベルを持つ」タスク)
function buildFilter(userId, name, status, labelIds) {
  let sql = ' WHERE user_id = ?';
  const params = [userId];
  if (name) {
    sql += ' AND name LIKE ?';
    params.push(`%${name}%`);
  }
  if (status !== null && status !== undefined) {
    sql += ' AND status = ?';
    params.push(status);
  }
  if (labelIds.length > 0) {
    const placeholders = labelIds.map(() => '?').join(',');
    sql += ` AND id IN (SELECT task_id FROM task_labels WHERE label_id IN (${placeholders}))`;
    params.push(...labelIds);
  }
  return { sql, params };
}

// v1(REST)向け: offsetページング。ラベルは一括JOINで取得する(N+1の意図的再現はしない。
// v1旧実装のN+1再現はbackend(Go)側の教材であり、この実装の主題ではないため)
async function listTasksOffset(pool, userId, name, status, labelIds, sortFinishedOn, limit, offset) {
  const filter = buildFilter(userId, name, status, labelIds);

  const [countRows] = await pool.execute(`SELECT COUNT(*) AS cnt FROM tasks${filter.sql}`, filter.params);
  const total = Number(countRows[0].cnt);

  const order =
    sortFinishedOn === 'asc' ? 'finished_on ASC' : sortFinishedOn === 'desc' ? 'finished_on DESC' : 'created_at DESC';
  const listSql = `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
                    FROM tasks${filter.sql} ORDER BY ${order} LIMIT ? OFFSET ?`;
  const [rows] = await pool.execute(listSql, [...filter.params, limit, offset]);

  const tasks = rows.map(rowToTask);
  await attachLabels(pool, tasks);
  return { tasks, total };
}

// v2(gRPC)向け: cursor(keyset)ページング。backend(Go)のUseCursor(id昇順固定)と同じ規約
async function listTasksCursor(pool, userId, name, status, labelIds, cursor, limit) {
  const filter = buildFilter(userId, name, status, labelIds);
  let sql = filter.sql;
  const params = [...filter.params];
  if (cursor > 0) {
    sql += ' AND id > ?';
    params.push(cursor);
  }
  const listSql = `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
                    FROM tasks${sql} ORDER BY id ASC LIMIT ?`;
  const [rows] = await pool.execute(listSql, [...params, limit]);
  const tasks = rows.map(rowToTask);
  await attachLabels(pool, tasks);
  return tasks;
}

// ユーザースコープ付きで1件取得(他人のtaskは見えない)
async function getTask(pool, id, userId) {
  const [rows] = await pool.execute(
    `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
     FROM tasks WHERE id = ? AND user_id = ?`,
    [id, userId],
  );
  if (rows.length === 0) return null;
  const tasks = [rowToTask(rows[0])];
  await attachLabels(pool, tasks);
  return tasks[0];
}

async function createTask(pool, userId, input, status) {
  const conn = await pool.getConnection();
  try {
    await conn.beginTransaction();
    const now = nowMysqlDatetime();
    const [result] = await conn.execute(
      `INSERT INTO tasks (name, description, status, finished_on, user_id, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?)`,
      [input.name, input.description ?? null, status, input.finishedOn, userId, now, now],
    );
    const taskId = result.insertId;
    await replaceLabels(conn, taskId, input.labelIds, now);
    await conn.commit();
    return taskId;
  } catch (e) {
    await conn.rollback();
    throw e;
  } finally {
    conn.release();
  }
}

// 戻り値: 更新できた場合true、対象行が(他人のtaskも含め)見つからない場合false
async function updateTask(pool, id, userId, input, status) {
  const conn = await pool.getConnection();
  try {
    await conn.beginTransaction();
    const now = nowMysqlDatetime();
    const [result] = await conn.execute(
      `UPDATE tasks SET name = ?, description = ?, status = ?, finished_on = ?, updated_at = ?
       WHERE id = ? AND user_id = ?`,
      [input.name, input.description ?? null, status, input.finishedOn, now, id, userId],
    );
    if (result.affectedRows === 0) {
      await conn.rollback();
      return false;
    }
    await replaceLabels(conn, id, input.labelIds, now);
    await conn.commit();
    return true;
  } catch (e) {
    await conn.rollback();
    throw e;
  } finally {
    conn.release();
  }
}

// 【backend(Go)の実装(internal/repository/task.go Delete)に合わせた設計判断】
// task_labelsにFK制約が無い(migrations/000004、ON DELETE CASCADE無し)ため、
// タスク削除時にtask_labelsの関連行を明示的に削除しないと孤立行が残る。
// backend-rustのdb.rs::delete_taskはこの後始末を欠いている(既知の差異、backend-js/README.md参照)。
// この実装はbackend(Go)の修正済みの挙動(同一トランザクションでtask_labelsも削除する)に合わせる
async function deleteTask(pool, id, userId) {
  const conn = await pool.getConnection();
  try {
    await conn.beginTransaction();
    const [result] = await conn.execute('DELETE FROM tasks WHERE id = ? AND user_id = ?', [id, userId]);
    if (result.affectedRows === 0) {
      await conn.rollback();
      return false;
    }
    await conn.execute('DELETE FROM task_labels WHERE task_id = ?', [id]);
    await conn.commit();
    return true;
  } catch (e) {
    await conn.rollback();
    throw e;
  } finally {
    conn.release();
  }
}

// 【既知バグの回帰防止】同一リクエスト内の重複label_id(例: [3,3,5])を重複排除してからINSERTする。
// 放置するとtask_labelsの(task_id,label_id)UNIQUE制約(migrations/000004)に違反し、
// 生のMySQLエラー(1062 Duplicate entry)がそのまま呼び出し元へ伝播する
// (CONTRACT.mdセクション23.1、5言語中4言語で見つかった実バグと同種)
async function replaceLabels(conn, taskId, labelIds, now) {
  await conn.execute('DELETE FROM task_labels WHERE task_id = ?', [taskId]);
  const seen = new Set();
  for (const labelId of labelIds) {
    if (seen.has(labelId)) continue;
    seen.add(labelId);
    await conn.execute(
      'INSERT INTO task_labels (task_id, label_id, created_at, updated_at) VALUES (?, ?, ?, ?)',
      [taskId, labelId, now, now],
    );
  }
}

async function attachLabels(pool, tasks) {
  if (tasks.length === 0) return;
  const ids = tasks.map((t) => t.id);
  const placeholders = ids.map(() => '?').join(',');
  const [rows] = await pool.execute(
    `SELECT task_labels.task_id AS task_id, labels.id AS id, labels.name AS name
     FROM task_labels JOIN labels ON labels.id = task_labels.label_id
     WHERE task_labels.task_id IN (${placeholders})`,
    ids,
  );
  const byTask = new Map();
  for (const row of rows) {
    if (!byTask.has(row.task_id)) byTask.set(row.task_id, []);
    byTask.get(row.task_id).push({ id: row.id, name: row.name });
  }
  for (const task of tasks) {
    task.labels = byTask.get(task.id) || [];
  }
}

// 外部公開API v1(offset、CONTRACT.mdセクション11)向け。フィルタは持たずuser_idのみで絞り込む
// backend(Go)のListOffsetForExternalAPIと同じソート順(created_at DESC, id DESC)
async function listTasksOffsetExternal(pool, userId, page, pageSize) {
  const [countRows] = await pool.execute('SELECT COUNT(*) AS cnt FROM tasks WHERE user_id = ?', [userId]);
  const total = Number(countRows[0].cnt);

  const offset = (page - 1) * pageSize;
  const [rows] = await pool.execute(
    `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
     FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
    [userId, pageSize, offset],
  );
  const tasks = rows.map(rowToTask);
  await attachLabels(pool, tasks);
  return { tasks, total };
}

// 外部公開API v2(keyset/cursor、CONTRACT.mdセクション11)向け
// backend(Go)のListCursorForExternalAPIと同じ絞り込み条件・ソート順
// ((created_at, id)の複合条件でOFFSETを使わない)
async function listTasksCursorExternal(pool, userId, after, limit) {
  let rows;
  if (after) {
    [rows] = await pool.execute(
      `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
       FROM tasks WHERE user_id = ? AND ((created_at < ?) OR (created_at = ? AND id < ?))
       ORDER BY created_at DESC, id DESC LIMIT ?`,
      [userId, after.createdAt, after.createdAt, after.id, limit],
    );
  } else {
    [rows] = await pool.execute(
      `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
       FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
      [userId, limit],
    );
  }
  const tasks = rows.map(rowToTask);
  await attachLabels(pool, tasks);
  return tasks;
}

function rowToTask(row) {
  return {
    id: row.id,
    name: row.name,
    description: row.description,
    status: statusFromDb(row.status) ?? TaskStatus.WAITING,
    finishedOn: row.finished_on,
    userId: row.user_id,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    labels: [],
  };
}

module.exports = {
  connect,
  findUserById,
  findUserByKeycloakSub,
  listTasksOffset,
  listTasksCursor,
  getTask,
  createTask,
  updateTask,
  deleteTask,
  listTasksOffsetExternal,
  listTasksCursorExternal,
};
