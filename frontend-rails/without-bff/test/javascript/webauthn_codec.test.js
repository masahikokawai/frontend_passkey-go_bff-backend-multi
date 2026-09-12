// node --test で実行する(with-bffのtest/javascript/webauthn_codec.test.jsと全く同じ、
// このアプリのapp/javascript/webauthn_codec.jsが同一実装であることの回帰確認)
import { test } from "node:test";
import assert from "node:assert/strict";
import { base64urlToBuffer, bufferToBase64url } from "../../app/javascript/webauthn_codec.js";

test("bufferToBase64url → base64urlToBuffer のround-tripで元のバイト列に戻る(長さ0〜10で網羅)", () => {
  for (let len = 0; len <= 10; len++) {
    const original = new Uint8Array(len);
    for (let i = 0; i < len; i++) original[i] = (i * 37 + 5) % 256;

    const encoded = bufferToBase64url(original.buffer);
    const decoded = new Uint8Array(base64urlToBuffer(encoded));

    assert.equal(decoded.length, original.length, `length mismatch at len=${len}`);
    assert.deepEqual(Array.from(decoded), Array.from(original), `bytes mismatch at len=${len}`);
  }
});

test("bufferToBase64urlの出力にパディング文字(=)や標準base64の記号(+/)が含まれない", () => {
  const bytesTriggeringPadding = new Uint8Array([0xff, 0xee, 0x3f, 0x3e]);
  const encoded = bufferToBase64url(bytesTriggeringPadding.buffer);
  assert.ok(!encoded.includes("="), `padding leaked: ${encoded}`);
  assert.ok(!encoded.includes("+"), `standard-base64 '+' leaked: ${encoded}`);
  assert.ok(!encoded.includes("/"), `standard-base64 '/' leaked: ${encoded}`);
});

test("base64urlToBufferは'-'/'_'(base64urlの記号)を正しく標準base64相当として解釈する", () => {
  const original = new Uint8Array([0xfb, 0xff, 0xbf]);
  const base64url = bufferToBase64url(original.buffer);
  const decoded = new Uint8Array(base64urlToBuffer(base64url));
  assert.deepEqual(Array.from(decoded), Array.from(original));
});
