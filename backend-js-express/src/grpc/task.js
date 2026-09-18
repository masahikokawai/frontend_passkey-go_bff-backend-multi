'use strict';

const grpc = require('@grpc/grpc-js');
const db = require('../db');
const { validateTaskInput, todayUtcIso, statusToString, statusFromString } = require('../model');
const { resolveUserId } = require('../auth');
const { logInfo } = require('../logging');

function grpcError(code, message) {
  const err = new Error(message);
  err.code = code;
  err.details = message;
  return err;
}

// "YYYY-MM-DD HH:MM:SS"(UTC、dateStrings:trueで取得した文字列) -> google.protobuf.Timestamp
function toTimestamp(mysqlDatetime) {
  const ms = Date.parse(`${mysqlDatetime.replace(' ', 'T')}Z`);
  return { seconds: Math.floor(ms / 1000), nanos: 0 };
}

function taskToPb(task) {
  return {
    id: task.id,
    name: task.name,
    description: task.description ?? undefined,
    status: statusToString(task.status),
    finished_on: task.finishedOn,
    labels: task.labels.map((l) => ({ id: l.id, name: l.name })),
    created_at: toTimestamp(task.createdAt),
    updated_at: toTimestamp(task.updatedAt),
  };
}

// backend(Go)のTaskServer.resolveUserID(gRPC v2版)・backend-rustのauthenticateと同じ:
// メタデータのauthorizationを検証し、未検証/未プロビジョニングをgRPCステータスへ変換する
async function authenticate(state, call) {
  const metadata = call.metadata.getMap();
  const authHeader = metadata.authorization;
  if (!authHeader) throw grpcError(grpc.status.UNAUTHENTICATED, 'unauthorized');
  const m = /^Bearer (.+)$/.exec(authHeader);
  if (!m) throw grpcError(grpc.status.UNAUTHENTICATED, 'unauthorized');

  let claims;
  try {
    claims = await state.dispatcher.verify(m[1]);
  } catch (e) {
    throw grpcError(grpc.status.UNAUTHENTICATED, 'unauthorized');
  }

  const resolved = await resolveUserId(state.pool, claims);
  if (!resolved.ok) throw grpcError(grpc.status.PERMISSION_DENIED, 'user not provisioned');
  return resolved.userId;
}

function parseFinishedOn(s) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s || '')) {
    throw grpcError(grpc.status.INVALID_ARGUMENT, `invalid finished_on: ${s}`);
  }
  return s;
}

function buildInput(req) {
  const finishedOn = parseFinishedOn(req.finished_on);
  return {
    name: req.name || '',
    description: req.description || null,
    status: req.status || '',
    finishedOn,
    labelIds: (req.label_ids || []).map(Number),
  };
}

function validate(input) {
  const result = validateTaskInput(input, todayUtcIso());
  if (result.error) throw grpcError(grpc.status.INVALID_ARGUMENT, result.error);
  return result.status;
}

function toGrpcError(e) {
  if (e && typeof e.code === 'number') return e;
  return grpcError(grpc.status.INTERNAL, e && e.message ? e.message : 'internal error');
}

function buildService(state) {
  return {
    async listTasks(call, callback) {
      try {
        const userId = await authenticate(state, call);
        const req = call.request;
        let status = null;
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
        const nextCursor = tasks.length < limit ? 0 : tasks[tasks.length - 1].id;
        callback(null, { tasks: tasks.map(taskToPb), next_cursor: nextCursor });
      } catch (e) {
        callback(toGrpcError(e));
      }
    },

    async getTask(call, callback) {
      try {
        const userId = await authenticate(state, call);
        const task = await db.getTask(state.pool, Number(call.request.id), userId);
        if (!task) throw grpcError(grpc.status.NOT_FOUND, 'task not found');
        callback(null, taskToPb(task));
      } catch (e) {
        callback(toGrpcError(e));
      }
    },

    async createTask(call, callback) {
      try {
        const userId = await authenticate(state, call);
        const input = buildInput(call.request);
        const status = validate(input);
        const id = await db.createTask(state.pool, userId, input, status);
        const task = await db.getTask(state.pool, id, userId);
        callback(null, taskToPb(task));
      } catch (e) {
        callback(toGrpcError(e));
      }
    },

    async updateTask(call, callback) {
      try {
        const userId = await authenticate(state, call);
        const id = Number(call.request.id);
        const input = buildInput(call.request);
        const status = validate(input);
        const updated = await db.updateTask(state.pool, id, userId, input, status);
        if (!updated) throw grpcError(grpc.status.NOT_FOUND, 'task not found');
        const task = await db.getTask(state.pool, id, userId);
        callback(null, taskToPb(task));
      } catch (e) {
        callback(toGrpcError(e));
      }
    },

    async deleteTask(call, callback) {
      try {
        const userId = await authenticate(state, call);
        const id = Number(call.request.id);
        const deleted = await db.deleteTask(state.pool, id, userId);
        if (!deleted) throw grpcError(grpc.status.NOT_FOUND, 'task not found');
        callback(null, {});
      } catch (e) {
        callback(toGrpcError(e));
      }
    },
  };
}

function grpcCodeName(code) {
  const entry = Object.entries(grpc.status).find(([, v]) => v === code);
  return entry ? entry[0] : String(code);
}

// method/実際のgRPCステータス/durationを1rpc1行のログとして出す(backend-rustのgrpc/task.rsの
// LoggingTaskGrpcServiceと同じ方針: ハンドラが返すエラー/成功をそのまま見るので、
// HTTP/2トレーラーを覗く必要が無く正確なステータスを記録できる)
function withLogging(service) {
  const wrapped = {};
  for (const [method, fn] of Object.entries(service)) {
    wrapped[method] = (call, callback) => {
      const start = process.hrtime.bigint();
      fn(call, (err, response) => {
        const durationMs = Number(process.hrtime.bigint() - start) / 1e6;
        const code = err ? (typeof err.code === 'number' ? err.code : grpc.status.UNKNOWN) : grpc.status.OK;
        logInfo('grpc request', { method, status: grpcCodeName(code), duration_ms: Math.round(durationMs) });
        callback(err, response);
      });
    };
  }
  return wrapped;
}

module.exports = { buildService, withLogging, taskToPb, toTimestamp };
