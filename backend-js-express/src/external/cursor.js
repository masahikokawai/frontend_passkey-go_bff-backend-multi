'use strict';

// backend(Go)のencodeExternalCursor/decodeExternalCursor(internal/service/task_external.go)と
// 同じ規約: (created_at, id)を"<RFC3339>|<id>"にしてbase64(パディング付き、URL-safeアルファベット。
// Goのbase64.URLEncoding・backend-rustのURL_SAFEと同じ)でエンコードした不透明文字列。
// パディングを揃えているのは、backend.task-languageの切り替え中にクライアントが
// 別言語のbackendへ同じcursorを渡した場合でも壊れず読めるようにするため

function encode(createdAtIso, id) {
  const raw = `${createdAtIso}|${id}`;
  const b64url = Buffer.from(raw, 'utf8').toString('base64url');
  const padLen = (4 - (b64url.length % 4)) % 4;
  return b64url + '='.repeat(padLen);
}

function decode(cursorRaw) {
  const cursor = cursorRaw.replace(/=+$/, '');
  let raw;
  try {
    raw = Buffer.from(cursor, 'base64url').toString('utf8');
  } catch (e) {
    throw `base64デコード失敗: ${e.message}`;
  }
  const idx = raw.indexOf('|');
  if (idx < 0) throw 'cursorの区切りが不正です';
  const createdAtStr = raw.slice(0, idx);
  const idStr = raw.slice(idx + 1);

  if (Number.isNaN(Date.parse(createdAtStr))) {
    throw `created_atの形式が不正です: ${createdAtStr}`;
  }
  if (!/^\d+$/.test(idStr)) {
    throw `idの形式が不正です: ${idStr}`;
  }

  return { createdAtIso: new Date(Date.parse(createdAtStr)).toISOString(), id: Number(idStr) };
}

module.exports = { encode, decode };
