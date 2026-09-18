// backend-js/src/grpc/task.jsの型付き移植。ロジックは変更していない。

import * as grpc from '@grpc/grpc-js';
import type { ServerUnaryCall, sendUnaryData, UntypedServiceImplementation } from '@grpc/grpc-js';
import * as db from '../db';
import { validateTaskInput, todayUtcIso, statusToString, statusFromString } from '../model';
import { resolveUserId } from '../auth';
import { logInfo } from '../logging';
import type { AppState, Task, TaskInput } from '../types';
import type {
  PbTask,
  PbTimestamp,
  ListTasksRequest,
  ListTasksResponse,
  GetTaskRequest,
  CreateTaskRequest,
  UpdateTaskRequest,
  DeleteTaskRequest,
  DeleteTaskResponse,
} from './messages';

interface GrpcError extends Error {
  code: number;
  details: string;
}

function grpcError(code: number, message: string): GrpcError {
  const err = new Error(message) as GrpcError;
  err.code = code;
  err.details = message;
  return err;
}

// "YYYY-MM-DD HH:MM:SS"(UTC、dateStrings:trueで取得した文字列) -> google.protobuf.Timestamp
function toTimestamp(mysqlDatetime: string): PbTimestamp {
  const ms = Date.parse(`${mysqlDatetime.replace(' ', 'T')}Z`);
  return { seconds: Math.floor(ms / 1000), nanos: 0 };
}

function taskToPb(task: Task): PbTask {
  return {
    id: task.id,
    name: task.name,
    description: task.description ?? undefined,
    status: statusToString(task.status) ?? '',
    finished_on: task.finishedOn,
    labels: task.labels.map((l) => ({ id: l.id, name: l.name })),
    created_at: toTimestamp(task.createdAt),
    updated_at: toTimestamp(task.updatedAt),
  };
}

// backend(Go)のTaskServer.resolveUserID(gRPC v2版)・backend-rust/backend-jsのauthenticateと同じ
async function authenticate(state: AppState, call: ServerUnaryCall<unknown, unknown>): Promise<number> {
  const metadata = call.metadata.getMap();
  const authHeader = metadata.authorization as string | undefined;
  if (!authHeader) throw grpcError(grpc.status.UNAUTHENTICATED, 'unauthorized');
  const m = /^Bearer (.+)$/.exec(authHeader);
  if (!m) throw grpcError(grpc.status.UNAUTHENTICATED, 'unauthorized');

  let claims;
  try {
    claims = await state.dispatcher.verify(m[1]!);
  } catch {
    throw grpcError(grpc.status.UNAUTHENTICATED, 'unauthorized');
  }

  const resolved = await resolveUserId(state.pool, claims);
  if (!resolved.ok) throw grpcError(grpc.status.PERMISSION_DENIED, 'user not provisioned');
  return resolved.userId;
}

function parseFinishedOn(s: string | undefined): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s || '')) {
    throw grpcError(grpc.status.INVALID_ARGUMENT, `invalid finished_on: ${s}`);
  }
  return s!;
}

function buildInput(req: CreateTaskRequest | UpdateTaskRequest): TaskInput {
  const finishedOn = parseFinishedOn(req.finished_on);
  return {
    name: req.name || '',
    description: req.description || null,
    status: req.status || '',
    finishedOn,
    labelIds: (req.label_ids || []).map(Number),
  };
}

function validate(input: TaskInput): number {
  const result = validateTaskInput(input, todayUtcIso());
  if (result.error !== undefined) throw grpcError(grpc.status.INVALID_ARGUMENT, result.error);
  return result.status;
}

function toGrpcError(e: unknown): GrpcError {
  if (e && typeof e === 'object' && 'code' in e && typeof (e as GrpcError).code === 'number') {
    return e as GrpcError;
  }
  return grpcError(grpc.status.INTERNAL, e instanceof Error ? e.message : 'internal error');
}

export function buildService(state: AppState): UntypedServiceImplementation {
  return {
    async listTasks(
      call: ServerUnaryCall<ListTasksRequest, ListTasksResponse>,
      callback: sendUnaryData<ListTasksResponse>,
    ) {
      try {
        const userId = await authenticate(state, call as unknown as ServerUnaryCall<unknown, unknown>);
        const req = call.request;
        let status: number | null = null;
        if (req.status) {
          status = statusFromString(req.status);
          if (status === null) throw grpcError(grpc.status.INVALID_ARGUMENT, `invalid status: ${req.status}`);
        }
        const limit = !req.limit || req.limit <= 0 ? 20 : req.limit;
        const tasks = await db.listTasksCursor(
          state.pool,
          userId,
          req.name || '',
          status,
          (req.label_ids || []).map(Number),
          req.cursor || 0,
          limit,
        );
        const nextCursor = tasks.length < limit ? 0 : tasks[tasks.length - 1]!.id;
        callback(null, { tasks: tasks.map(taskToPb), next_cursor: nextCursor });
      } catch (e) {
        callback(toGrpcError(e), null);
      }
    },

    async getTask(call: ServerUnaryCall<GetTaskRequest, PbTask>, callback: sendUnaryData<PbTask>) {
      try {
        const userId = await authenticate(state, call as unknown as ServerUnaryCall<unknown, unknown>);
        const task = await db.getTask(state.pool, Number(call.request.id), userId);
        if (!task) throw grpcError(grpc.status.NOT_FOUND, 'task not found');
        callback(null, taskToPb(task));
      } catch (e) {
        callback(toGrpcError(e), null);
      }
    },

    async createTask(call: ServerUnaryCall<CreateTaskRequest, PbTask>, callback: sendUnaryData<PbTask>) {
      try {
        const userId = await authenticate(state, call as unknown as ServerUnaryCall<unknown, unknown>);
        const input = buildInput(call.request);
        const status = validate(input);
        const id = await db.createTask(state.pool, userId, input, status);
        const task = await db.getTask(state.pool, id, userId);
        callback(null, taskToPb(task!));
      } catch (e) {
        callback(toGrpcError(e), null);
      }
    },

    async updateTask(call: ServerUnaryCall<UpdateTaskRequest, PbTask>, callback: sendUnaryData<PbTask>) {
      try {
        const userId = await authenticate(state, call as unknown as ServerUnaryCall<unknown, unknown>);
        const id = Number(call.request.id);
        const input = buildInput(call.request);
        const status = validate(input);
        const updated = await db.updateTask(state.pool, id, userId, input, status);
        if (!updated) throw grpcError(grpc.status.NOT_FOUND, 'task not found');
        const task = await db.getTask(state.pool, id, userId);
        callback(null, taskToPb(task!));
      } catch (e) {
        callback(toGrpcError(e), null);
      }
    },

    async deleteTask(
      call: ServerUnaryCall<DeleteTaskRequest, DeleteTaskResponse>,
      callback: sendUnaryData<DeleteTaskResponse>,
    ) {
      try {
        const userId = await authenticate(state, call as unknown as ServerUnaryCall<unknown, unknown>);
        const id = Number(call.request.id);
        const deleted = await db.deleteTask(state.pool, id, userId);
        if (!deleted) throw grpcError(grpc.status.NOT_FOUND, 'task not found');
        callback(null, {});
      } catch (e) {
        callback(toGrpcError(e), null);
      }
    },
  };
}

function grpcCodeName(code: number): string {
  const entry = Object.entries(grpc.status).find(([, v]) => v === code);
  return entry ? entry[0]! : String(code);
}

// method/実際のgRPCステータス/durationを1rpc1行のログとして出す(backend-rust/backend-jsと同じ方針)
export function withLogging(service: UntypedServiceImplementation): UntypedServiceImplementation {
  const wrapped: UntypedServiceImplementation = {};
  for (const [method, fn] of Object.entries(service)) {
    wrapped[method] = ((call: ServerUnaryCall<unknown, unknown>, callback: sendUnaryData<unknown>) => {
      const start = process.hrtime.bigint();
      (fn as (c: unknown, cb: (err: GrpcError | null, res: unknown) => void) => void)(
        call,
        (err: GrpcError | null, response: unknown) => {
          const durationMs = Number(process.hrtime.bigint() - start) / 1e6;
          const code = err ? (typeof err.code === 'number' ? err.code : grpc.status.UNKNOWN) : grpc.status.OK;
          logInfo('grpc request', { method, status: grpcCodeName(code), duration_ms: Math.round(durationMs) });
          callback(err, response);
        },
      );
    }) as UntypedServiceImplementation[string];
  }
  return wrapped;
}
