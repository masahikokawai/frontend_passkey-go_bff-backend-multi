'use strict';

// backend(Go)の internal/handler/v1/render.go の renderServiceError と
// 各ハンドラのエラー分岐(invalid_request/invalid_status/invalid_finished_on)を1つにまとめたもの
// JSON形状・ステータスコードは他言語実装(backend-rust等)と1文字も変えていない
class RestError extends Error {
  constructor(kind, status, body) {
    super(kind);
    this.kind = kind;
    this.status = status;
    this.body = body;
  }
}

const unauthorized = () => new RestError('unauthorized', 401, { error: 'unauthorized' });
const userNotProvisioned = () => new RestError('user_not_provisioned', 403, { error: 'user_not_provisioned' });
const invalidRequest = () => new RestError('invalid_request', 400, { error: 'invalid_request' });
const invalidId = () => new RestError('invalid_id', 400, { error: 'invalid_id' });
const invalidStatus = () => new RestError('invalid_status', 422, { error: 'invalid_status' });
const invalidFinishedOn = () => new RestError('invalid_finished_on', 422, { error: 'invalid_finished_on' });
const validationError = (message) =>
  new RestError('validation_error', 422, { error: 'validation_error', message });
const notFound = () => new RestError('not_found', 404, { error: 'not_found' });
const internal = () => new RestError('internal_server_error', 500, { error: 'internal_server_error' });

// 外部公開API(CONTRACT.mdセクション11)専用
const invalidToken = () => new RestError('invalid_token', 401, { error: 'invalid_token' });
const clientNotAllowed = () => new RestError('client_not_allowed', 403, { error: 'client_not_allowed' });
const userIdRequired = () => new RestError('user_id_required', 400, { error: 'user_id is required' });
const invalidUserId = () => new RestError('invalid_user_id', 400, { error: 'invalid user_id' });
const invalidCursor = (message) =>
  new RestError('invalid_cursor', 422, { error: `validation_error: ${message}` });

// Expressのエラーハンドリングミドルウェア。RestError以外は500として扱う
function errorHandler() {
  // eslint-disable-next-line no-unused-vars
  return (err, req, res, next) => {
    if (err instanceof RestError) {
      res.status(err.status).json(err.body);
      return;
    }
    res.status(500).json({ error: 'internal_server_error' });
  };
}

module.exports = {
  RestError,
  unauthorized,
  userNotProvisioned,
  invalidRequest,
  invalidId,
  invalidStatus,
  invalidFinishedOn,
  validationError,
  notFound,
  internal,
  invalidToken,
  clientNotAllowed,
  userIdRequired,
  invalidUserId,
  invalidCursor,
  errorHandler,
};
