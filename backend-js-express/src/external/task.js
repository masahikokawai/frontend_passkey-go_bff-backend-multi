'use strict';

const db = require('../db');
const errors = require('../error');
const cursorMod = require('./cursor');
const { authenticate } = require('./authenticate');
const { statusToString } = require('../model');
const { mysqlDatetimeToRfc3339, isoToMysqlDatetime } = require('../time');

function wrap(fn) {
  return (req, res, next) => Promise.resolve(fn(req, res)).catch(next);
}

// backend(Go)のtaskDTOToJSON(internal/handler/external/task.go)と同じ形状。
// 内部CRUDのレスポンスと同様に**user_idを含めない**点に注意
function taskToJson(task) {
  return {
    id: task.id,
    name: task.name,
    description: task.description,
    status: statusToString(task.status),
    finished_on: task.finishedOn,
    labels: task.labels.map((l) => ({ id: l.id, name: l.name })),
    created_at: mysqlDatetimeToRfc3339(task.createdAt),
    updated_at: mysqlDatetimeToRfc3339(task.updatedAt),
  };
}

// GET /external/v1/tasks
// `backend.external-tasks-pagination-v2`(全言語で共有する1つのFeature Flag)のON/OFFで
// offsetページング(v1)/keysetページング(v2)を切り替える(backend(Go)のTaskHandler.Listと同じ)
function list(state) {
  return wrap(async (req, res) => {
    await authenticate(state, req);

    const userIdStr = req.query.user_id;
    if (!userIdStr) throw errors.userIdRequired();
    if (!/^\d+$/.test(userIdStr)) throw errors.invalidUserId();
    const userId = Number(userIdStr);

    if (state.paginationV2Flag.get()) {
      await listV2(state, userId, req.query, res);
    } else {
      await listV1(state, userId, req.query, res);
    }
  });
}

async function listV1(state, userId, q, res) {
  const page = q.page && Number(q.page) >= 1 ? Number(q.page) : 1;
  const pageSize = q.page_size && Number(q.page_size) >= 1 ? Number(q.page_size) : 10;

  const { tasks, total } = await db.listTasksOffsetExternal(state.pool, userId, page, pageSize);
  res.json({ tasks: tasks.map(taskToJson), page, page_size: pageSize, total });
}

async function listV2(state, userId, q, res) {
  const limit = q.limit && Number(q.limit) >= 1 ? Number(q.limit) : 10;

  let after = null;
  if (q.cursor) {
    let decoded;
    try {
      decoded = cursorMod.decode(q.cursor);
    } catch (msg) {
      throw errors.invalidCursor(String(msg));
    }
    const mysqlDt = isoToMysqlDatetime(decoded.createdAtIso);
    if (!mysqlDt) throw errors.invalidCursor('created_atの形式が不正です');
    after = { createdAt: mysqlDt, id: decoded.id };
  }

  const tasks = await db.listTasksCursorExternal(state.pool, userId, after, limit);

  let nextCursor = null;
  if (tasks.length === limit) {
    const last = tasks[tasks.length - 1];
    nextCursor = cursorMod.encode(mysqlDatetimeToRfc3339(last.createdAt), last.id);
  }
  res.json({ tasks: tasks.map(taskToJson), next_cursor: nextCursor, limit });
}

module.exports = { list, taskToJson };
