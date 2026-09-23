// backend-js/src/logging.jsの型付き移植。LOG_LEVEL対応(logDebug/shouldLog)を追加した点のみ
// JS版と異なる(このLOG_LEVEL対応自体は両者で同じロジック)。

import type { Request, Response, NextFunction } from 'express';

// LOG_LEVEL環境変数("debug"/"info"/"warn"/"error"、既定"info"、backend(Go)/bff/gateway(Go)と
// 同じ規約)でログの詳細度を切り替える。追加の依存ライブラリは入れず、既存のconsole.log/warn
// ベースの最小実装をそのまま拡張する。モジュールはimportで1度しか評価されないため、
// このモジュール直下での読み取りが「起動時に1度だけ」を満たす
const LOG_LEVELS: Record<string, number> = { debug: 10, info: 20, warn: 30, error: 40 };
const currentLevel = LOG_LEVELS[(process.env.LOG_LEVEL || 'info').toLowerCase()] ?? LOG_LEVELS.info!;

export function shouldLog(level: string): boolean {
  return (LOG_LEVELS[level] ?? LOG_LEVELS.info!) >= currentLevel;
}

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

// LOG_LEVEL=debugのときだけ出る詳細ログ(認証で解決したuser_id/issuer、JWKSキャッシュの
// 再取得、REST/外部APIで解析したページングパラメータ)。backend-python/app/logging_middleware.py
// 等が`log.debug(...)`で出しているものと同じ位置づけのDEBUG専用ログ
export function logDebug(message: string, fields?: Record<string, unknown>): void {
  if (!shouldLog('debug')) return;
  // eslint-disable-next-line no-console
  console.log(`level=debug msg=${JSON.stringify(message)} ${fieldsToString(fields)}`.trim());
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
