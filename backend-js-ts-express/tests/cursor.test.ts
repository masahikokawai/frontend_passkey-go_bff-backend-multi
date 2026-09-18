// backend-js/tests/cursor.test.jsの型付き移植。テスト内容は変更していない。

import test from 'node:test';
import assert from 'node:assert/strict';
import * as cursor from '../src/external/cursor';

test('encode/decode round trip', () => {
  const createdAtIso = '2026-09-09T12:34:56+00:00';
  const encoded = cursor.encode(createdAtIso, 42);
  const decoded = cursor.decode(encoded);
  assert.equal(new Date(decoded.createdAtIso).getTime(), new Date(createdAtIso).getTime());
  assert.equal(decoded.id, 42);
});

test('decode rejects garbage', () => {
  assert.throws(() => cursor.decode('not-a-valid-cursor!!!'));
});

test('decode rejects wrong separator count', () => {
  const bogus = Buffer.from('just-one-part', 'utf8').toString('base64url');
  assert.throws(() => cursor.decode(bogus));
});

test('encoded cursor is padded like Rust/Go base64 URL-safe output', () => {
  const encoded = cursor.encode('2026-09-09T12:34:56+00:00', 1);
  assert.equal(encoded.length % 4, 0);
});
