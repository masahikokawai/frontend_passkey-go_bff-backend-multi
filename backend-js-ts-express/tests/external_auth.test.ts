// backend-js-express/tests/external_auth.test.jsの型付き移植。テスト内容は変更していない。
// backend-java/ExternalAuthTest.java・backend-python/test_external_auth.pyの移植でもある。
// 外部公開API(CONTRACT.mdセクション11)のauthenticate()、
// (1) KeycloakのJWKSで署名検証、(2) azpクレームが期待クライアントIDと一致するか、を検証する。
// DB・実サーバーは不要(モックJWKSサーバーのみ、jwks_verifier.test.tsと同じ設計)

import test from 'node:test';
import assert from 'node:assert/strict';
import type { Request } from 'express';
import { authenticate } from '../src/external/authenticate';
import { JwksVerifier, LOCAL_HMAC_ISSUER, LOCAL_RSA_ISSUER } from '../src/auth/jwt';
import type { ExternalState } from '../src/types';
import { createMockJwksServer } from './support/mock_jwks_server';
import { generateRsaKeyPair, makeRsaToken, makeHmacToken } from './support/test_token_helper';
import type { RestError } from '../src/error';

const KEYCLOAK_ISSUER = 'https://keycloak.example';
const EXTERNAL_CLIENT_ID = 'external-api-client';
const HMAC_SECRET = 'test-secret-at-least-32-bytes-long!!';

function fakeReq(authorizationHeader: string | undefined): Request {
  return { get: (name: string) => (name === 'authorization' ? authorizationHeader : undefined) } as unknown as Request;
}

// pool/paginationV2Flagはこのテストの認証チェックでは使われないため、テスト専用の最小限の状態を作る
function externalState(keycloakVerifier: JwksVerifier): ExternalState {
  return { keycloakVerifier, expectedClientId: EXTERNAL_CLIENT_ID } as unknown as ExternalState;
}

function assertRestError(err: unknown, kind: string, status: number): true {
  const e = err as RestError;
  assert.equal(e.kind, kind);
  assert.equal(e.status, status);
  return true;
}

test('accepts a Keycloak token with a matching azp', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const state = externalState(new JwksVerifier(mockJwks.jwksUrl(), KEYCLOAK_ISSUER, 'backend'));
    const token = makeRsaToken(privateKey, 'kid-1', KEYCLOAK_ISSUER, 'backend', 'some-keycloak-sub', 3600, 'RS256', EXTERNAL_CLIENT_ID);

    await assert.doesNotReject(() => authenticate(state, fakeReq(`Bearer ${token}`)));
  } finally {
    await mockJwks.close();
  }
});

test('rejects a missing Authorization header', async () => {
  const state = externalState(new JwksVerifier('http://127.0.0.1:1/jwks', KEYCLOAK_ISSUER, 'backend'));
  await assert.rejects(
    () => authenticate(state, fakeReq(undefined)),
    (err) => assertRestError(err, 'unauthorized', 401),
  );
});

test('rejects a malformed (non-Bearer) Authorization header', async () => {
  const state = externalState(new JwksVerifier('http://127.0.0.1:1/jwks', KEYCLOAK_ISSUER, 'backend'));
  await assert.rejects(
    () => authenticate(state, fakeReq('not-a-bearer-header')),
    (err) => assertRestError(err, 'unauthorized', 401),
  );
});

test('rejects the wrong azp', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const state = externalState(new JwksVerifier(mockJwks.jwksUrl(), KEYCLOAK_ISSUER, 'backend'));
    const token = makeRsaToken(privateKey, 'kid-1', KEYCLOAK_ISSUER, 'backend', 'sub', 3600, 'RS256', 'some-other-client');

    await assert.rejects(
      () => authenticate(state, fakeReq(`Bearer ${token}`)),
      (err) => assertRestError(err, 'client_not_allowed', 403),
    );
  } finally {
    await mockJwks.close();
  }
});

test('rejects a missing azp claim', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const state = externalState(new JwksVerifier(mockJwks.jwksUrl(), KEYCLOAK_ISSUER, 'backend'));
    // azp無し(付与しない)のトークン
    const token = makeRsaToken(privateKey, 'kid-1', KEYCLOAK_ISSUER, 'backend', 'sub', 3600);

    await assert.rejects(
      () => authenticate(state, fakeReq(`Bearer ${token}`)),
      (err) => assertRestError(err, 'client_not_allowed', 403),
    );
  } finally {
    await mockJwks.close();
  }
});

// 【最も見落としやすいケース】ローカルHMAC発行のトークンは、署名自体は別issuer向けの
// verifierでは検証できない(そもそもkeycloakVerifierはKeycloakのissuerしか受け付けない)。
// azpが偶然一致していても、外部公開APIはKeycloak発行(Client Credentials Grant)のみ許可する
test('rejects a locally-issued HMAC token even with a correct azp', async () => {
  const state = externalState(new JwksVerifier('http://127.0.0.1:1/jwks', KEYCLOAK_ISSUER, 'backend'));
  const token = makeHmacToken(HMAC_SECRET, LOCAL_HMAC_ISSUER, 'backend', '1', 3600, 'HS256', EXTERNAL_CLIENT_ID);

  await assert.rejects(
    () => authenticate(state, fakeReq(`Bearer ${token}`)),
    (err) => assertRestError(err, 'invalid_token', 401),
  );
});

// 同様に、ローカルRSA発行(bff自身の/.well-known/jwks.json)のトークンも、
// 正しく署名されRS256/JWKSでかつazpが一致していても、issuerがKeycloakでない限り拒否される
test('rejects a locally-issued RSA token even with a correct azp', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const state = externalState(new JwksVerifier(mockJwks.jwksUrl(), KEYCLOAK_ISSUER, 'backend'));
    const token = makeRsaToken(privateKey, 'kid-1', LOCAL_RSA_ISSUER, 'backend', '1', 3600, 'RS256', EXTERNAL_CLIENT_ID);

    await assert.rejects(
      () => authenticate(state, fakeReq(`Bearer ${token}`)),
      (err) => assertRestError(err, 'invalid_token', 401),
    );
  } finally {
    await mockJwks.close();
  }
});
