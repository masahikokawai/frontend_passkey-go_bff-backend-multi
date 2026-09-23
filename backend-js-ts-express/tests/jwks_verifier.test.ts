// backend-js-express/tests/jwks_verifier.test.jsの型付き移植。テスト内容は変更していない。
// backend-java/JwksVerifierTest.java・backend-python/test_jwks_verifier.pyの移植でもある。
// 自プロセス内蔵のモックJWKSサーバーを使い、実際のRS256署名検証・kidキャッシュ・
// 未知kid時の再取得ロジックを検証する。DB・Keycloak・bff実プロセスは不要

import test from 'node:test';
import assert from 'node:assert/strict';
import { JwksVerifier, VerifyError } from '../src/auth/jwt';
import { createMockJwksServer } from './support/mock_jwks_server';
import { generateRsaKeyPair, makeRsaToken } from './support/test_token_helper';

const ISSUER = 'https://issuer.example';

test('accepts a valid token signed with a known key', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const verifier = new JwksVerifier(mockJwks.jwksUrl(), ISSUER, 'backend');

    const token = makeRsaToken(privateKey, 'kid-1', ISSUER, 'backend', '99', 3600);
    const claims = await verifier.verify(token);
    assert.equal(claims.sub, '99');
    assert.equal(claims.iss, ISSUER);
  } finally {
    await mockJwks.close();
  }
});

test('an unknown kid triggers a refresh and succeeds once present', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    // JwksVerifier生成時点ではまだ鍵ゼロ件 -> 最初の検証はkid不一致で1回だけ再取得を試みる
    const verifier = new JwksVerifier(mockJwks.jwksUrl(), ISSUER, 'backend');
    mockJwks.addKey('kid-2', publicKey);

    const token = makeRsaToken(privateKey, 'kid-2', ISSUER, 'backend', '1', 3600);
    const claims = await verifier.verify(token);
    assert.equal(claims.sub, '1');
  } finally {
    await mockJwks.close();
  }
});

test('a kid that is still unknown after refresh is rejected', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-registered', publicKey);
    const verifier = new JwksVerifier(mockJwks.jwksUrl(), ISSUER, 'backend');

    // JWKSには存在しないkidで署名したトークン -> 再取得しても見つからず拒否される
    const token = makeRsaToken(privateKey, 'kid-does-not-exist', ISSUER, 'backend', '1', 3600);
    await assert.rejects(() => verifier.verify(token), VerifyError);
  } finally {
    await mockJwks.close();
  }
});

test('the wrong issuer is rejected even with a valid signature', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const verifier = new JwksVerifier(mockJwks.jwksUrl(), ISSUER, 'backend');

    const token = makeRsaToken(privateKey, 'kid-1', 'https://different-issuer.example', 'backend', '1', 3600);
    await assert.rejects(() => verifier.verify(token), VerifyError);
  } finally {
    await mockJwks.close();
  }
});

test('the wrong audience is rejected', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const verifier = new JwksVerifier(mockJwks.jwksUrl(), ISSUER, 'backend');

    const token = makeRsaToken(privateKey, 'kid-1', ISSUER, 'someone-else', '1', 3600);
    await assert.rejects(() => verifier.verify(token), VerifyError);
  } finally {
    await mockJwks.close();
  }
});

test('an expired token is rejected', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const verifier = new JwksVerifier(mockJwks.jwksUrl(), ISSUER, 'backend');

    const token = makeRsaToken(privateKey, 'kid-1', ISSUER, 'backend', '1', -3600);
    await assert.rejects(() => verifier.verify(token), VerifyError);
  } finally {
    await mockJwks.close();
  }
});

// アルゴリズム混同攻撃対策: ヘッダのalgがRS256以外なら拒否する
test('rejects an unexpected algorithm', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const verifier = new JwksVerifier(mockJwks.jwksUrl(), ISSUER, 'backend');

    const token = makeRsaToken(privateKey, 'kid-1', ISSUER, 'backend', '1', 3600, 'RS512');
    await assert.rejects(() => verifier.verify(token), VerifyError);
  } finally {
    await mockJwks.close();
  }
});
