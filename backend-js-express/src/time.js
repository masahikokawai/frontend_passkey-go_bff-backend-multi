'use strict';

// MySQLのDATETIME列を常にUTC基準の文字列のまま扱うためのヘルパー群
// (mysql2をdateStrings:trueで接続し、Dateオブジェクトへ変換しないことで
// Node実行環境のローカルタイムゾーンが介在する余地を最初から無くしている。
// これは過去に発見された「finished_onの過去日判定がタイムゾーンでずれる」バグ
// (CONTRACT.mdセクション23.1)と同種の問題をそもそも起こさないための設計判断)

// "2026-09-09 12:34:56" (MySQL DATETIME文字列) -> "2026-09-09T12:34:56+00:00" (RFC3339)
function mysqlDatetimeToRfc3339(mysqlDatetime) {
  return `${mysqlDatetime.replace(' ', 'T')}+00:00`;
}

// RFC3339文字列 -> "2026-09-09 12:34:56" (MySQL DATETIME文字列、UTC基準)
function isoToMysqlDatetime(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  return d.toISOString().slice(0, 19).replace('T', ' ');
}

function nowMysqlDatetime() {
  return new Date().toISOString().slice(0, 19).replace('T', ' ');
}

module.exports = { mysqlDatetimeToRfc3339, isoToMysqlDatetime, nowMysqlDatetime };
