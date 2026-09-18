// backend-js/src/rest/task.jsの型付き移植。ロジックは変更していない。

import type { Request, Response, NextFunction, RequestHandler } from 'express';
import * as db from '../db';
import * as errors from '../error';
import { authenticate } from './authenticate';
import { validateTaskInput, todayUtcIso, statusToString, statusFromString } from '../model';
import type { AppState, Task, TaskInput } from '../types';

function wrap(fn: (req: Request, res: Response) => Promise<void>): RequestHandler {
  return (req: Request, res: Response, next: NextFunction) => {
    fn(req, res).catch(next);
  };
}

function parseId(raw: string): number {
  if (!/^\d+$/.test(raw)) throw errors.invalidId();
  return Number(raw);
}

// backend(Go)のparseUintListQueryと同じく、パースに失敗した要素は無視して続行する
export function parseLabelIds(raw: string | undefined): number[] {
  if (!raw) return [];
  return raw
    .split(',')
    .map((p) => p.trim())
    .filter((p) => /^\d+$/.test(p))
    .map((p) => Number(p));
}

// CONTRACT.mdセクション5.1のJSON形状(スネークケース)。
// 【backend(Go)の実際の挙動に合わせた既知の差異】taskDTOToJSON(backend/internal/handler/v1/task.go)は
// user_idをレスポンスに含めていない。
export function taskToJson(task: Task): Record<string, unknown> {
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

export function list(state: AppState) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const q = req.query as Record<string, string | undefined>;

    let status: number | null = null;
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

export function get(state: AppState) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const id = parseId(req.params.id!);
    const task = await db.getTask(state.pool, id, userId);
    if (!task) throw errors.notFound();
    res.json(taskToJson(task));
  });
}

// backend(Go)のtaskRequestBody(binding:"required")と同じ「空文字も必須違反」の扱い
export function toTaskInput(body: Record<string, unknown>): TaskInput {
  const name = (body.name as string) ?? '';
  const status = (body.status as string) ?? '';
  const finishedOn = (body.finished_on as string) ?? '';
  if (!name || !status || !finishedOn) throw errors.invalidRequest();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(finishedOn)) throw errors.invalidFinishedOn();

  // カレンダー上有効な日付かも確認する(例: 2026-02-30を弾く)
  const [y, m, d] = finishedOn.split('-').map(Number) as [number, number, number];
  const check = new Date(Date.UTC(y, m - 1, d));
  if (check.getUTCFullYear() !== y || check.getUTCMonth() !== m - 1 || check.getUTCDate() !== d) {
    throw errors.invalidFinishedOn();
  }

  const labelIds = Array.isArray(body.label_ids) ? (body.label_ids as unknown[]).map(Number) : [];
  return { name, description: (body.description as string) ?? null, status, finishedOn, labelIds };
}

export function create(state: AppState) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const input = toTaskInput(req.body || {});

    const result = validateTaskInput(input, todayUtcIso());
    if (result.error !== undefined) throw errors.validationError(result.error);

    const id = await db.createTask(state.pool, userId, input, result.status);
    const task = await db.getTask(state.pool, id, userId);
    res.status(201).json(taskToJson(task!));
  });
}

export function update(state: AppState) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const id = parseId(req.params.id!);
    const input = toTaskInput(req.body || {});

    const result = validateTaskInput(input, todayUtcIso());
    if (result.error !== undefined) throw errors.validationError(result.error);

    const updated = await db.updateTask(state.pool, id, userId, input, result.status);
    if (!updated) throw errors.notFound();
    const task = await db.getTask(state.pool, id, userId);
    res.json(taskToJson(task!));
  });
}

export function remove(state: AppState) {
  return wrap(async (req, res) => {
    const userId = await authenticate(state, req);
    const id = parseId(req.params.id!);
    const deleted = await db.deleteTask(state.pool, id, userId);
    if (!deleted) throw errors.notFound();
    res.status(204).send();
  });
}
