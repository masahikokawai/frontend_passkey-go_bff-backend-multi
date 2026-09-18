// backend-js/src/time.jsの型付き移植。ロジックは変更していない。
// MySQLのDATETIME列を常にUTC基準の文字列のまま扱うためのヘルパー群。

// "2026-09-09 12:34:56" (MySQL DATETIME文字列) -> "2026-09-09T12:34:56+00:00" (RFC3339)
export function mysqlDatetimeToRfc3339(mysqlDatetime: string): string {
  return `${mysqlDatetime.replace(' ', 'T')}+00:00`;
}

// RFC3339文字列 -> "2026-09-09 12:34:56" (MySQL DATETIME文字列、UTC基準)
export function isoToMysqlDatetime(iso: string): string | null {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  return d.toISOString().slice(0, 19).replace('T', ' ');
}

export function nowMysqlDatetime(): string {
  return new Date().toISOString().slice(0, 19).replace('T', ' ');
}
