'use strict';

// リクエスト単位のログ(method/path/status/duration_ms)。backend-rust/src/logging.rsと同じ方針:
// LOG_LEVEL=debug無しでも既定でどのリクエストを処理したかログから追えるようにする

// LOG_LEVEL環境変数("debug"/"info"/"warn"/"error"、既定"info"、backend(Go)/bff/gateway(Go)と
// 同じ規約)でログの詳細度を切り替える。追加の依存ライブラリは入れず(backend-python/backend-c/
// backend-elixirも標準機能のみで済ませている方針と同じ)、既存のconsole.log/warnベースの
// 最小実装をそのまま拡張する。Node.jsの`require`はモジュールを1度しか評価しないため、
// このモジュール直下での読み取りが「起動時に1度だけ」を満たす
const LOG_LEVELS = { debug: 10, info: 20, warn: 30, error: 40 };
const currentLevel = LOG_LEVELS[(process.env.LOG_LEVEL || 'info').toLowerCase()] ?? LOG_LEVELS.info;

function shouldLog(level) {
  return LOG_LEVELS[level] >= currentLevel;
}

function fieldsToString(fields) {
  return Object.entries(fields || {})
    .map(([k, v]) => `${k}=${JSON.stringify(v)}`)
    .join(' ');
}

function logInfo(message, fields) {
  // eslint-disable-next-line no-console
  console.log(`level=info msg=${JSON.stringify(message)} ${fieldsToString(fields)}`.trim());
}

function logWarn(message, fields) {
  // eslint-disable-next-line no-console
  console.warn(`level=warn msg=${JSON.stringify(message)} ${fieldsToString(fields)}`.trim());
}

// LOG_LEVEL=debugのときだけ出る詳細ログ(認証で解決したuser_id/issuer、JWKSキャッシュの
// 再取得、REST/外部APIで解析したページングパラメータ)。backend-python/app/logging_middleware.py
// 等が`log.debug(...)`で出しているものと同じ位置づけのDEBUG専用ログ
function logDebug(message, fields) {
  if (!shouldLog('debug')) return;
  // eslint-disable-next-line no-console
  console.log(`level=debug msg=${JSON.stringify(message)} ${fieldsToString(fields)}`.trim());
}

function requestLogger() {
  return (req, res, next) => {
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

module.exports = { logInfo, logWarn, logDebug, shouldLog, requestLogger };
