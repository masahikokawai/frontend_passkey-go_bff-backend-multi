// backend-js-express/tests/hmac_and_dispatcher.test.jsの型付き移植。テスト内容は変更していない。
// backend-java/HmacAndDispatcherTest.java・backend-python/test_hmac_and_dispatcher.pyの移植でもある。
// HmacVerifier単体とDispatcherの振り分けロジックを検証する(DB不要)

import test from 'node:test';
import assert from 'node:assert/strict';
import { HmacVerifier, Dispatcher, isLocalIssuer, LOCAL_HMAC_ISSUER, LOCAL_RSA_ISSUER, VerifyError } from '../src/auth/jwt';
import { makeHmacToken, makeAlgNoneToken } from './support/test_token_helper';

// HS256は最低256bit(32バイト)の鍵長が実質的に必要になるため、十分な長さのテスト用秘密鍵を使う
const SECRET = 'test-secret-at-least-32-bytes-long!!';

test('isLocalIssuer matches HMAC and RSA local issuers only', () => {
  assert.equal(isLocalIssuer(LOCAL_HMAC_ISSUER), true);
  assert.equal(isLocalIssuer(LOCAL_RSA_ISSUER), true);
  assert.equal(isLocalIssuer('http://localhost:8082/realms/training'), false);
  assert.equal(isLocalIssuer(''), false);
});

test('HmacVerifier accepts a valid token', async () => {
  const token = makeHmacToken(SECRET, LOCAL_HMAC_ISSUER, 'backend', '42', 3600);
  const verifier = new HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, 'backend');
  const claims = await verifier.verify(token);
  assert.equal(claims.sub, '42');
  assert.equal(claims.iss, LOCAL_HMAC_ISSUER);
});

test('HmacVerifier rejects a token signed with the wrong secret', async () => {
  const token = makeHmacToken(SECRET, LOCAL_HMAC_ISSUER, 'backend', '42', 3600);
  const verifier = new HmacVerifier('different-secret-also-32-bytes-long!', LOCAL_HMAC_ISSUER, 'backend');
  await assert.rejects(() => verifier.verify(token), VerifyError);
});

test('HmacVerifier rejects an expired token', async () => {
  const token = makeHmacToken(SECRET, LOCAL_HMAC_ISSUER, 'backend', '42', -3600);
  const verifier = new HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, 'backend');
  await assert.rejects(() => verifier.verify(token), VerifyError);
});

test('HmacVerifier rejects the wrong audience', async () => {
  const token = makeHmacToken(SECRET, LOCAL_HMAC_ISSUER, 'someone-else', '42', 3600);
  const verifier = new HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, 'backend');
  await assert.rejects(() => verifier.verify(token), VerifyError);
});

test('HmacVerifier rejects the wrong issuer', async () => {
  const token = makeHmacToken(SECRET, 'some-other-issuer', 'backend', '42', 3600);
  const verifier = new HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, 'backend');
  await assert.rejects(() => verifier.verify(token), VerifyError);
});

// 【セキュリティ上重要な確認】ヘッダのalgが期待(HS256)と異なる場合は、
// 仮に鍵が正しくても拒否されること(アルゴリズム混同攻撃対策)
test('HmacVerifier rejects an unexpected algorithm', async () => {
  const token = makeHmacToken(SECRET, LOCAL_HMAC_ISSUER, 'backend', '42', 3600, 'HS384');
  const verifier = new HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, 'backend');
  await assert.rejects(() => verifier.verify(token), VerifyError);
});

test('HmacVerifier rejects alg=none tokens', async () => {
  const token = makeAlgNoneToken(LOCAL_HMAC_ISSUER, 'backend', '42', 3600);
  const verifier = new HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, 'backend');
  await assert.rejects(() => verifier.verify(token), VerifyError);
});

test('Dispatcher routes by issuer and rejects unknown issuers', async () => {
  const dispatcher = new Dispatcher().register(LOCAL_HMAC_ISSUER, new HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, 'backend'));

  const goodToken = makeHmacToken(SECRET, LOCAL_HMAC_ISSUER, 'backend', '7', 3600);
  const claims = await dispatcher.verify(goodToken);
  assert.equal(claims.sub, '7');

  const unknownIssuerToken = makeHmacToken(SECRET, 'unknown-issuer', 'backend', '7', 3600);
  await assert.rejects(() => dispatcher.verify(unknownIssuerToken), VerifyError);
});

test('Dispatcher rejects a malformed token', async () => {
  const dispatcher = new Dispatcher().register(LOCAL_HMAC_ISSUER, new HmacVerifier(SECRET, LOCAL_HMAC_ISSUER, 'backend'));
  await assert.rejects(() => dispatcher.verify('not-a-jwt'), VerifyError);
});
