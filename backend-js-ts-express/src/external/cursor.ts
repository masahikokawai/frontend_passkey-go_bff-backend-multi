// backend-js/src/external/cursor.jsの型付き移植。ロジックは変更していない。
// (created_at, id)を"<RFC3339>|<id>"にしてbase64url(パディング付き)でエンコードした不透明文字列。

export function encode(createdAtIso: string, id: number): string {
  const raw = `${createdAtIso}|${id}`;
  const b64url = Buffer.from(raw, 'utf8').toString('base64url');
  const padLen = (4 - (b64url.length % 4)) % 4;
  return b64url + '='.repeat(padLen);
}

export interface DecodedCursor {
  createdAtIso: string;
  id: number;
}

export function decode(cursorRaw: string): DecodedCursor {
  const cursor = cursorRaw.replace(/=+$/, '');
  let raw: string;
  try {
    raw = Buffer.from(cursor, 'base64url').toString('utf8');
  } catch (e) {
    throw `base64デコード失敗: ${(e as Error).message}`;
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
