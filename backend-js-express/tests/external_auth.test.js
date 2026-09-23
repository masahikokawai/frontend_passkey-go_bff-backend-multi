'use strict';

// backend-java/ExternalAuthTest.java・backend-python/test_external_auth.pyの移植。
// 外部公開API(CONTRACT.mdセクション11)のauthenticate()、
// (1) KeycloakのJWKSで署名検証、(2) azpクレームが期待クライアントIDと一致するか、を検証する。
// DB・実サーバーは不要(モックJWKSサーバーのみ、jwks_verifier.test.jsと同じ設計)
const test = require('node:test');
const assert = require('node:assert/strict');
const { authenticate } = require('../src/external/authenticate');
const { JwksVerifier, LOCAL_HMAC_ISSUER, LOCAL_RSA_ISSUER } = require('../src/auth/jwt');
const { createMockJwksServer } = require('./support/mock_jwks_server');
const { generateRsaKeyPair, makeRsaToken, makeHmacToken } = require('./support/test_token_helper');

const KEYCLOAK_ISSUER = 'https://keycloak.example';
const EXTERNAL_CLIENT_ID = 'external-api-client';
const HMAC_SECRET = 'test-secret-at-least-32-bytes-long!!';

function fakeReq(authorizationHeader) {
  return { get: (name) => (name === 'authorization' ? authorizationHeader : undefined) };
}

test('accepts a Keycloak token with a matching azp', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const state = {
      keycloakVerifier: new JwksVerifier(mockJwks.jwksUrl(), KEYCLOAK_ISSUER, 'backend'),
      expectedClientId: EXTERNAL_CLIENT_ID,
    };
    const token = makeRsaToken(privateKey, 'kid-1', KEYCLOAK_ISSUER, 'backend', 'some-keycloak-sub', 3600, 'RS256', EXTERNAL_CLIENT_ID);

    await assert.doesNotReject(() => authenticate(state, fakeReq(`Bearer ${token}`)));
  } finally {
    await mockJwks.close();
  }
});

test('rejects a missing Authorization header', async () => {
  const state = { keycloakVerifier: new JwksVerifier('http://127.0.0.1:1/jwks', KEYCLOAK_ISSUER, 'backend'), expectedClientId: EXTERNAL_CLIENT_ID };
  await assert.rejects(
    () => authenticate(state, fakeReq(undefined)),
    (err) => {
      assert.equal(err.kind, 'unauthorized');
      assert.equal(err.status, 401);
      return true;
    },
  );
});

test('rejects a malformed (non-Bearer) Authorization header', async () => {
  const state = { keycloakVerifier: new JwksVerifier('http://127.0.0.1:1/jwks', KEYCLOAK_ISSUER, 'backend'), expectedClientId: EXTERNAL_CLIENT_ID };
  await assert.rejects(
    () => authenticate(state, fakeReq('not-a-bearer-header')),
    (err) => {
      assert.equal(err.kind, 'unauthorized');
      assert.equal(err.status, 401);
      return true;
    },
  );
});

test('rejects the wrong azp', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const state = {
      keycloakVerifier: new JwksVerifier(mockJwks.jwksUrl(), KEYCLOAK_ISSUER, 'backend'),
      expectedClientId: EXTERNAL_CLIENT_ID,
    };
    const token = makeRsaToken(privateKey, 'kid-1', KEYCLOAK_ISSUER, 'backend', 'sub', 3600, 'RS256', 'some-other-client');

    await assert.rejects(
      () => authenticate(state, fakeReq(`Bearer ${token}`)),
      (err) => {
        assert.equal(err.kind, 'client_not_allowed');
        assert.equal(err.status, 403);
        return true;
      },
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
    const state = {
      keycloakVerifier: new JwksVerifier(mockJwks.jwksUrl(), KEYCLOAK_ISSUER, 'backend'),
      expectedClientId: EXTERNAL_CLIENT_ID,
    };
    // azp無し(付与しない)のトークン
    const token = makeRsaToken(privateKey, 'kid-1', KEYCLOAK_ISSUER, 'backend', 'sub', 3600);

    await assert.rejects(
      () => authenticate(state, fakeReq(`Bearer ${token}`)),
      (err) => {
        assert.equal(err.kind, 'client_not_allowed');
        assert.equal(err.status, 403);
        return true;
      },
    );
  } finally {
    await mockJwks.close();
  }
});

// 【最も見落としやすいケース】ローカルHMAC発行のトークンは、署名自体は別issuer向けの
// verifierでは検証できない(そもそもkeycloakVerifierはKeycloakのissuerしか受け付けない)。
// azpが偶然一致していても、外部公開APIはKeycloak発行(Client Credentials Grant)のみ許可する
test('rejects a locally-issued HMAC token even with a correct azp', async () => {
  const state = {
    keycloakVerifier: new JwksVerifier('http://127.0.0.1:1/jwks', KEYCLOAK_ISSUER, 'backend'),
    expectedClientId: EXTERNAL_CLIENT_ID,
  };
  const token = makeHmacToken(HMAC_SECRET, LOCAL_HMAC_ISSUER, 'backend', '1', 3600, 'HS256', EXTERNAL_CLIENT_ID);

  await assert.rejects(
    () => authenticate(state, fakeReq(`Bearer ${token}`)),
    (err) => {
      assert.equal(err.kind, 'invalid_token');
      assert.equal(err.status, 401);
      return true;
    },
  );
});

// 同様に、ローカルRSA発行(bff自身の/.well-known/jwks.json)のトークンも、
// 正しく署名されRS256/JWKSでかつazpが一致していても、issuerがKeycloakでない限り拒否される
test('rejects a locally-issued RSA token even with a correct azp', async () => {
  const { publicKey, privateKey } = generateRsaKeyPair();
  const mockJwks = await createMockJwksServer();
  try {
    mockJwks.addKey('kid-1', publicKey);
    const state = {
      keycloakVerifier: new JwksVerifier(mockJwks.jwksUrl(), KEYCLOAK_ISSUER, 'backend'),
      expectedClientId: EXTERNAL_CLIENT_ID,
    };
    const token = makeRsaToken(privateKey, 'kid-1', LOCAL_RSA_ISSUER, 'backend', '1', 3600, 'RS256', EXTERNAL_CLIENT_ID);

    await assert.rejects(
      () => authenticate(state, fakeReq(`Bearer ${token}`)),
      (err) => {
        assert.equal(err.kind, 'invalid_token');
        assert.equal(err.status, 401);
        return true;
      },
    );
  } finally {
    await mockJwks.close();
  }
});
