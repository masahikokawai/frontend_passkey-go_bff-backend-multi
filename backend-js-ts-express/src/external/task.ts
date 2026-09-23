// backend-js/src/external/task.jsの型付き移植。ロジックは変更していない。

import type { Request, Response, NextFunction, RequestHandler } from 'express';
import * as db from '../db';
import * as errors from '../error';
import * as cursorMod from './cursor';
import { authenticate } from './authenticate';
import { statusToString } from '../model';
import { mysqlDatetimeToRfc3339, isoToMysqlDatetime } from '../time';
import type { ExternalState, Task } from '../types';
import { logDebug } from '../logging';

function wrap(fn: (req: Request, res: Response) => Promise<void>): RequestHandler {
  return (req: Request, res: Response, next: NextFunction) => {
    fn(req, res).catch(next);
  };
}

// backend(Go)のtaskDTOToJSON(internal/handler/external/task.go)と同じ形状。
// 内部CRUDのレスポンスと同様に**user_idを含めない**点に注意
export function taskToJson(task: Task): Record<string, unknown> {
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
// offsetページング(v1)/keysetページング(v2)を切り替える
export function list(state: ExternalState) {
  return wrap(async (req, res) => {
    await authenticate(state, req);

    const userIdStr = req.query.user_id as string | undefined;
    if (!userIdStr) throw errors.userIdRequired();
    if (!/^\d+$/.test(userIdStr)) throw errors.invalidUserId();
    const userId = Number(userIdStr);

    if (state.paginationV2Flag.get()) {
      await listV2(state, userId, req.query as Record<string, string | undefined>, res);
    } else {
      await listV1(state, userId, req.query as Record<string, string | undefined>, res);
    }
  });
}

async function listV1(
  state: ExternalState,
  userId: number,
  q: Record<string, string | undefined>,
  res: Response,
): Promise<void> {
  const page = q.page && Number(q.page) >= 1 ? Number(q.page) : 1;
  const pageSize = q.page_size && Number(q.page_size) >= 1 ? Number(q.page_size) : 10;
  logDebug('list_tasks_external', { user_id: userId, page, page_size: pageSize });

  const { tasks, total } = await db.listTasksOffsetExternal(state.pool, userId, page, pageSize);
  res.json({ tasks: tasks.map(taskToJson), page, page_size: pageSize, total });
}

async function listV2(
  state: ExternalState,
  userId: number,
  q: Record<string, string | undefined>,
  res: Response,
): Promise<void> {
  const limit = q.limit && Number(q.limit) >= 1 ? Number(q.limit) : 10;
  logDebug('list_tasks_external', { user_id: userId, cursor: q.cursor || null, limit });

  let after: { createdAt: string; id: number } | null = null;
  if (q.cursor) {
    let decoded: cursorMod.DecodedCursor;
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

  let nextCursor: string | null = null;
  if (tasks.length === limit) {
    const last = tasks[tasks.length - 1]!;
    nextCursor = cursorMod.encode(mysqlDatetimeToRfc3339(last.createdAt), last.id);
  }
  res.json({ tasks: tasks.map(taskToJson), next_cursor: nextCursor, limit });
}
