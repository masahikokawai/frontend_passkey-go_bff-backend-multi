// backend-js/src/logging.jsの型付き移植。ロジックは変更していない。

import type { Request, Response, NextFunction } from 'express';

function fieldsToString(fields?: Record<string, unknown>): string {
  return Object.entries(fields || {})
    .map(([k, v]) => `${k}=${JSON.stringify(v)}`)
    .join(' ');
}

export function logInfo(message: string, fields?: Record<string, unknown>): void {
  // eslint-disable-next-line no-console
  console.log(`level=info msg=${JSON.stringify(message)} ${fieldsToString(fields)}`.trim());
}

export function logWarn(message: string, fields?: Record<string, unknown>): void {
  // eslint-disable-next-line no-console
  console.warn(`level=warn msg=${JSON.stringify(message)} ${fieldsToString(fields)}`.trim());
}

export function requestLogger() {
  return (req: Request, res: Response, next: NextFunction): void => {
    const start = process.hrtime.bigint();
    res.on('finish', () => {
      const durationMs = Number(process.hrtime.bigint() - start) / 1e6;
      logInfo('request', {
        method: req.method,
        path: req.path,
        status: res.statusCode,
        duration_ms: Math.round(durationMs),
      });
    });
    next();
  };
}
