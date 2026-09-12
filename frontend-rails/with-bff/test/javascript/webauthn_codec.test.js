// node --test で実行する(追加の依存無し、Node組み込みのテストランナー・assertを使う)
// package.jsonは無いため、"type": "module"はNode 22の.mjs拡張子判定またはpackage.json省略時のデフォルト(CommonJS)に依存する
// import文を使うため拡張子は.mjsではないが、
// Node 22はpackage.json不在ディレクトリでも.jsファイル内のESM構文を検出して実行できる
// (--experimental-detect-moduleが既定で有効、Node 22時点)
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
  // WebAuthnのcredential.toJSON()相当のペイロードはURLセーフ(base64url)である必要があり、
  // ここが標準base64のまま漏れると、bff-rails側のデコード(Ruby側もbase64urlを期待)がサイレントに壊れる
  //
  // このプロジェクトで既に
  // 「hmacの署名鍵をbase64urlで扱うべきところを標準base64のままにしていた」
  // 類の事故が繰り返し起きているため、変換方式の取り違えは明示的にテストで押さえておく
  const bytesTriggeringPadding = new Uint8Array([0xff, 0xee, 0x3f, 0x3e]); // "+"/"/"を生みやすいバイト列
  const encoded = bufferToBase64url(bytesTriggeringPadding.buffer);
  assert.ok(!encoded.includes("="), `padding leaked: ${encoded}`);
  assert.ok(!encoded.includes("+"), `standard-base64 '+' leaked: ${encoded}`);
  assert.ok(!encoded.includes("/"), `standard-base64 '/' leaked: ${encoded}`);
});

test("base64urlToBufferは'-'/'_'(base64urlの記号)を正しく標準base64相当として解釈する", () => {
  // 標準base64で"+"になるはずの箇所が"-"、"/"になるはずの箇所が"_"として届く前提を確認する
  // (WebAuthn仕様上のchallenge/credential idは常にこの表記のため)
  const original = new Uint8Array([0xfb, 0xff, 0xbf]); // encodeすると"+"/"/"を含みうるバイト列
  const base64url = bufferToBase64url(original.buffer);
  const decoded = new Uint8Array(base64urlToBuffer(base64url));
  assert.deepEqual(Array.from(decoded), Array.from(original));
});
