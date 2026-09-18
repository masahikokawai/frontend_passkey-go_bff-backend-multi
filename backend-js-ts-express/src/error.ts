// backend-js/src/error.jsの型付き移植。JSON形状・ステータスコードは1文字も変えていない。

import type { Request, Response, NextFunction } from 'express';

export class RestError extends Error {
  kind: string;
  status: number;
  body: Record<string, unknown>;

  constructor(kind: string, status: number, body: Record<string, unknown>) {
    super(kind);
    this.kind = kind;
    this.status = status;
    this.body = body;
  }
}

export const unauthorized = (): RestError => new RestError('unauthorized', 401, { error: 'unauthorized' });
export const userNotProvisioned = (): RestError =>
  new RestError('user_not_provisioned', 403, { error: 'user_not_provisioned' });
export const invalidRequest = (): RestError => new RestError('invalid_request', 400, { error: 'invalid_request' });
export const invalidId = (): RestError => new RestError('invalid_id', 400, { error: 'invalid_id' });
export const invalidStatus = (): RestError => new RestError('invalid_status', 422, { error: 'invalid_status' });
export const invalidFinishedOn = (): RestError =>
  new RestError('invalid_finished_on', 422, { error: 'invalid_finished_on' });
export const validationError = (message: string): RestError =>
  new RestError('validation_error', 422, { error: 'validation_error', message });
export const notFound = (): RestError => new RestError('not_found', 404, { error: 'not_found' });
export const internal = (): RestError => new RestError('internal_server_error', 500, { error: 'internal_server_error' });

// 外部公開API(CONTRACT.mdセクション11)専用
export const invalidToken = (): RestError => new RestError('invalid_token', 401, { error: 'invalid_token' });
export const clientNotAllowed = (): RestError =>
  new RestError('client_not_allowed', 403, { error: 'client_not_allowed' });
export const userIdRequired = (): RestError =>
  new RestError('user_id_required', 400, { error: 'user_id is required' });
export const invalidUserId = (): RestError => new RestError('invalid_user_id', 400, { error: 'invalid user_id' });
export const invalidCursor = (message: string): RestError =>
  new RestError('invalid_cursor', 422, { error: `validation_error: ${message}` });

export function errorHandler() {
  return (err: unknown, _req: Request, res: Response, _next: NextFunction): void => {
    if (err instanceof RestError) {
      res.status(err.status).json(err.body);
      return;
    }
    res.status(500).json({ error: 'internal_server_error' });
  };
}
