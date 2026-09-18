'use strict';

// リクエスト単位のログ(method/path/status/duration_ms)。backend-rust/src/logging.rsと同じ方針:
// LOG_LEVEL=debug無しでも既定でどのリクエストを処理したかログから追えるようにする

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

module.exports = { logInfo, logWarn, requestLogger };
