// backend-js/src/db.jsの型付き移植。ロジックは変更していない。

import mysql from 'mysql2/promise';
import type { Pool, PoolConnection, RowDataPacket } from 'mysql2/promise';
import { URL } from 'node:url';
import { TaskStatus, statusFromDb } from './model';
import { nowMysqlDatetime } from './time';
import type { Task, TaskInput, Label } from './types';

interface UserRow {
  id: number;
  email: string;
  name: string;
}

export interface AppUser {
  id: number;
  email: string;
  name: string;
}

function buildPoolConfig(dsn: string): mysql.PoolOptions {
  const url = new URL(dsn);
  return {
    host: url.hostname,
    port: url.port ? parseInt(url.port, 10) : 3306,
    user: url.username ? decodeURIComponent(url.username) : 'root',
    password: url.password ? decodeURIComponent(url.password) : '',
    database: url.pathname.replace(/^\//, ''),
    // MySQLのDATE/DATETIMEを常にUTC基準の文字列のまま扱う(タイムゾーン不整合バグの回帰防止、
    // backend-js/src/db.jsのコメント参照)
    dateStrings: true,
    waitForConnections: true,
    connectionLimit: 10,
  };
}

export function connect(dsn: string): Pool {
  return mysql.createPool(buildPoolConfig(dsn));
}

export async function findUserById(pool: Pool, id: number): Promise<AppUser | null> {
  const [rows] = await pool.execute<(UserRow & RowDataPacket)[]>(
    'SELECT id, email, name FROM users WHERE id = ?',
    [id],
  );
  return rows[0] ? rowToUser(rows[0]) : null;
}

export async function findUserByKeycloakSub(pool: Pool, sub: string): Promise<AppUser | null> {
  const [rows] = await pool.execute<(UserRow & RowDataPacket)[]>(
    `SELECT users.id AS id, users.email AS email, users.name AS name
     FROM users JOIN user_keycloaks ON user_keycloaks.user_id = users.id
     WHERE user_keycloaks.keycloak_sub = ?`,
    [sub],
  );
  return rows[0] ? rowToUser(rows[0]) : null;
}

function rowToUser(row: UserRow): AppUser {
  return { id: row.id, email: row.email, name: row.name };
}

function buildFilter(
  userId: number,
  name: string,
  status: number | null,
  labelIds: number[],
): { sql: string; params: Array<string | number> } {
  let sql = ' WHERE user_id = ?';
  const params: Array<string | number> = [userId];
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

export async function listTasksOffset(
  pool: Pool,
  userId: number,
  name: string,
  status: number | null,
  labelIds: number[],
  sortFinishedOn: string,
  limit: number,
  offset: number,
): Promise<{ tasks: Task[]; total: number }> {
  const filter = buildFilter(userId, name, status, labelIds);

  const [countRows] = await pool.execute<RowDataPacket[]>(
    `SELECT COUNT(*) AS cnt FROM tasks${filter.sql}`,
    filter.params,
  );
  const total = Number(countRows[0]!.cnt);

  const order =
    sortFinishedOn === 'asc' ? 'finished_on ASC' : sortFinishedOn === 'desc' ? 'finished_on DESC' : 'created_at DESC';
  const listSql = `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
                    FROM tasks${filter.sql} ORDER BY ${order} LIMIT ? OFFSET ?`;
  const [rows] = await pool.execute<RowDataPacket[]>(listSql, [...filter.params, limit, offset]);

  const tasks = rows.map(rowToTask);
  await attachLabels(pool, tasks);
  return { tasks, total };
}

export async function listTasksCursor(
  pool: Pool,
  userId: number,
  name: string,
  status: number | null,
  labelIds: number[],
  cursor: number,
  limit: number,
): Promise<Task[]> {
  const filter = buildFilter(userId, name, status, labelIds);
  let sql = filter.sql;
  const params: Array<string | number> = [...filter.params];
  if (cursor > 0) {
    sql += ' AND id > ?';
    params.push(cursor);
  }
  const listSql = `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
                    FROM tasks${sql} ORDER BY id ASC LIMIT ?`;
  const [rows] = await pool.execute<RowDataPacket[]>(listSql, [...params, limit]);
  const tasks = rows.map(rowToTask);
  await attachLabels(pool, tasks);
  return tasks;
}

export async function getTask(pool: Pool, id: number, userId: number): Promise<Task | null> {
  const [rows] = await pool.execute<RowDataPacket[]>(
    `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
     FROM tasks WHERE id = ? AND user_id = ?`,
    [id, userId],
  );
  if (rows.length === 0) return null;
  const tasks = [rowToTask(rows[0]!)];
  await attachLabels(pool, tasks);
  return tasks[0]!;
}

export async function createTask(pool: Pool, userId: number, input: TaskInput, status: number): Promise<number> {
  const conn: PoolConnection = await pool.getConnection();
  try {
    await conn.beginTransaction();
    const now = nowMysqlDatetime();
    const [result] = await conn.execute(
      `INSERT INTO tasks (name, description, status, finished_on, user_id, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?)`,
      [input.name, input.description ?? null, status, input.finishedOn, userId, now, now],
    );
    const taskId = (result as mysql.ResultSetHeader).insertId;
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

export async function updateTask(
  pool: Pool,
  id: number,
  userId: number,
  input: TaskInput,
  status: number,
): Promise<boolean> {
  const conn: PoolConnection = await pool.getConnection();
  try {
    await conn.beginTransaction();
    const now = nowMysqlDatetime();
    const [result] = await conn.execute(
      `UPDATE tasks SET name = ?, description = ?, status = ?, finished_on = ?, updated_at = ?
       WHERE id = ? AND user_id = ?`,
      [input.name, input.description ?? null, status, input.finishedOn, now, id, userId],
    );
    if ((result as mysql.ResultSetHeader).affectedRows === 0) {
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

// 【backend(Go)の実装(internal/repository/task.go Delete)に合わせた設計判断、backend-jsと同じ】
// task_labelsにFK制約が無い(migrations/000004、ON DELETE CASCADE無し)ため、
// タスク削除時にtask_labelsの関連行を明示的に削除しないと孤立行が残る。
// backend-rustのdb.rs::delete_taskはこの後始末を欠いている(既知の差異、backend-js/README.md参照)。
export async function deleteTask(pool: Pool, id: number, userId: number): Promise<boolean> {
  const conn: PoolConnection = await pool.getConnection();
  try {
    await conn.beginTransaction();
    const [result] = await conn.execute('DELETE FROM tasks WHERE id = ? AND user_id = ?', [id, userId]);
    if ((result as mysql.ResultSetHeader).affectedRows === 0) {
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
async function replaceLabels(conn: PoolConnection, taskId: number, labelIds: number[], now: string): Promise<void> {
  await conn.execute('DELETE FROM task_labels WHERE task_id = ?', [taskId]);
  const seen = new Set<number>();
  for (const labelId of labelIds) {
    if (seen.has(labelId)) continue;
    seen.add(labelId);
    await conn.execute(
      'INSERT INTO task_labels (task_id, label_id, created_at, updated_at) VALUES (?, ?, ?, ?)',
      [taskId, labelId, now, now],
    );
  }
}

async function attachLabels(pool: Pool, tasks: Task[]): Promise<void> {
  if (tasks.length === 0) return;
  const ids = tasks.map((t) => t.id);
  const placeholders = ids.map(() => '?').join(',');
  const [rows] = await pool.execute<RowDataPacket[]>(
    `SELECT task_labels.task_id AS task_id, labels.id AS id, labels.name AS name
     FROM task_labels JOIN labels ON labels.id = task_labels.label_id
     WHERE task_labels.task_id IN (${placeholders})`,
    ids,
  );
  const byTask = new Map<number, Label[]>();
  for (const row of rows) {
    const taskId = row.task_id as number;
    if (!byTask.has(taskId)) byTask.set(taskId, []);
    byTask.get(taskId)!.push({ id: row.id as number, name: row.name as string });
  }
  for (const task of tasks) {
    task.labels = byTask.get(task.id) || [];
  }
}

export async function listTasksOffsetExternal(
  pool: Pool,
  userId: number,
  page: number,
  pageSize: number,
): Promise<{ tasks: Task[]; total: number }> {
  const [countRows] = await pool.execute<RowDataPacket[]>('SELECT COUNT(*) AS cnt FROM tasks WHERE user_id = ?', [
    userId,
  ]);
  const total = Number(countRows[0]!.cnt);

  const offset = (page - 1) * pageSize;
  const [rows] = await pool.execute<RowDataPacket[]>(
    `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
     FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
    [userId, pageSize, offset],
  );
  const tasks = rows.map(rowToTask);
  await attachLabels(pool, tasks);
  return { tasks, total };
}

export interface ExternalCursorAfter {
  createdAt: string;
  id: number;
}

export async function listTasksCursorExternal(
  pool: Pool,
  userId: number,
  after: ExternalCursorAfter | null,
  limit: number,
): Promise<Task[]> {
  let rows: RowDataPacket[];
  if (after) {
    [rows] = await pool.execute<RowDataPacket[]>(
      `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
       FROM tasks WHERE user_id = ? AND ((created_at < ?) OR (created_at = ? AND id < ?))
       ORDER BY created_at DESC, id DESC LIMIT ?`,
      [userId, after.createdAt, after.createdAt, after.id, limit],
    );
  } else {
    [rows] = await pool.execute<RowDataPacket[]>(
      `SELECT id, name, description, status, finished_on, user_id, created_at, updated_at
       FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
      [userId, limit],
    );
  }
  const tasks = rows.map(rowToTask);
  await attachLabels(pool, tasks);
  return tasks;
}

function rowToTask(row: RowDataPacket): Task {
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
