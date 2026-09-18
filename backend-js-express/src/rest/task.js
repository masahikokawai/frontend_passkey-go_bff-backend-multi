'use strict';

const db = require('../db');
const errors = require('../error');
const { authenticate } = require('./authenticate');
const { validateTaskInput, todayUtcIso, statusToString, statusFromString } = require('../model');

function wrap(fn) {
  return (req, res, next) => Promise.resolve(fn(req, res)).catch(next);
}

function parseId(raw) {
  if (!/^\d+$/.test(raw)) throw errors.invalidId();
  return Number(raw);
}

// backend(Go)のparseUintListQueryと同じく、パースに失敗した要素は無視して続行する
function parseLabelIds(raw) {
  if (!raw) return [];
  return raw
    .split(',')
    .map((p) => p.trim())
    .filter((p) => /^\d+$/.test(p))
    .map((p) => Number(p));
}

// CONTRACT.mdセクション5.1のJSON形状(スネークケース)。
// 【backend(Go)の実際の挙動に合わせた既知の差異】taskDTOToJSON(backend/internal/handler/v1/task.go)は
// user_idをレスポンスに含めていない。ワイヤー契約パリティの原則により、ドキュメントの例文ではなく
// 実際のGo実装の挙動に合わせている
function taskToJson(task) {
  return {
    id: task.id,
    name: task.name,
    description: task.description,
    status: statusToString(task.status),
    finished_on: task.finishedOn,
    labels: task.labels.map((l) => ({ id: l.id, name: l.name })),
    created_at: `${task.createdAt.replace(' ', 'T')}+00:00`,
    updated_at: `${task.updatedAt.replace(' ', 'T')}+00:00`,
  };
}

function list(state) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const q = req.query;

    let status = null;
    if (q.status) {
      status = statusFromString(q.status);
      if (status === null) throw errors.invalidStatus();
    }
    const labelIds = parseLabelIds(q.label_ids);
    const limit = q.limit !== undefined ? parseInt(q.limit, 10) : 20;
    const offset = q.offset !== undefined ? parseInt(q.offset, 10) : 0;

    const { tasks, total } = await db.listTasksOffset(
      state.pool,
      userId,
      q.name || '',
      status,
      labelIds,
      q.sort || '',
      limit,
      offset,
    );
    res.json({ tasks: tasks.map(taskToJson), total, limit, offset });
  });
}

function get(state) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const id = parseId(req.params.id);
    const task = await db.getTask(state.pool, id, userId);
    if (!task) throw errors.notFound();
    res.json(taskToJson(task));
  });
}

// backend(Go)のtaskRequestBody(binding:"required")と同じ「空文字も必須違反」の扱い
function toTaskInput(body) {
  const name = body.name ?? '';
  const status = body.status ?? '';
  const finishedOn = body.finished_on ?? '';
  if (!name || !status || !finishedOn) throw errors.invalidRequest();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(finishedOn)) throw errors.invalidFinishedOn();

  // カレンダー上有効な日付かも確認する(例: 2026-02-30を弾く。chronoのNaiveDate::parse_from_strと
  // 同じ厳密さをここで再現する)
  const [y, m, d] = finishedOn.split('-').map(Number);
  const check = new Date(Date.UTC(y, m - 1, d));
  if (check.getUTCFullYear() !== y || check.getUTCMonth() !== m - 1 || check.getUTCDate() !== d) {
    throw errors.invalidFinishedOn();
  }

  const labelIds = Array.isArray(body.label_ids) ? body.label_ids.map(Number) : [];
  return { name, description: body.description ?? null, status, finishedOn, labelIds };
}

function create(state) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const input = toTaskInput(req.body || {});

    const result = validateTaskInput(input, todayUtcIso());
    if (result.error) throw errors.validationError(result.error);

    const id = await db.createTask(state.pool, userId, input, result.status);
    const task = await db.getTask(state.pool, id, userId);
    res.status(201).json(taskToJson(task));
  });
}

function update(state) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const id = parseId(req.params.id);
    const input = toTaskInput(req.body || {});

    const result = validateTaskInput(input, todayUtcIso());
    if (result.error) throw errors.validationError(result.error);

    const updated = await db.updateTask(state.pool, id, userId, input, result.status);
    if (!updated) throw errors.notFound();
    const task = await db.getTask(state.pool, id, userId);
    res.json(taskToJson(task));
  });
}

function remove(state) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const id = parseId(req.params.id);
    const deleted = await db.deleteTask(state.pool, id, userId);
    if (!deleted) throw errors.notFound();
    res.status(204).send();
  });
}

module.exports = { list, get, create, update, remove, taskToJson, parseLabelIds, toTaskInput };
