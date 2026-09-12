// node --test で実行する(webauthn_codec.test.jsと同じ、追加の依存無し)
// fetch/navigator.credentialsを注入できる設計にしたことで、DOM無し・ブラウザ無しで
// ログイン/登録の儀式ロジック(成功/各失敗パターン)を検証できる
import { test } from "node:test";
import assert from "node:assert/strict";
import { loginWithPasskey, registerPasskeyCeremony, credentialToJSON } from "../../app/javascript/passkey_client.js";

// base64urlToBuffer/bufferToBase64urlはブラウザ標準のatob/btoaに依存するため、
// Node実行用に最小限のポリフィルを用意する(webauthn_codec.test.jsは変換の往復しか
// 検証しないため不要だったが、こちらはoptions.challenge等の実際の変換を経由する)
if (typeof globalThis.atob === "undefined") {
  globalThis.atob = (s) => Buffer.from(s, "base64").toString("binary");
  globalThis.btoa = (s) => Buffer.from(s, "binary").toString("base64");
}

function fakeResponse({ ok, status, body }) {
  return { ok, status, json: async () => body };
}

function fakeCredential({ isRegistration }) {
  const base = {
    id: "cred-id",
    rawId: new Uint8Array([1, 2, 3]).buffer,
    type: "public-key",
    getClientExtensionResults: () => ({}),
  };
  if (isRegistration) {
    return {
      ...base,
      response: {
        attestationObject: new Uint8Array([9, 9]).buffer,
        clientDataJSON: new Uint8Array([8, 8]).buffer,
        getTransports: () => ["internal"],
      },
    };
  }
  return {
    ...base,
    response: {
      authenticatorData: new Uint8Array([7, 7]).buffer,
      clientDataJSON: new Uint8Array([8, 8]).buffer,
      signature: new Uint8Array([6, 6]).buffer,
      userHandle: new Uint8Array([5, 5]).buffer,
    },
  };
}

test("credentialToJSON: 登録由来(attestationObjectを持つ)credentialを正しい形状に変換する", () => {
  const json = credentialToJSON(fakeCredential({ isRegistration: true }));
  assert.equal(json.id, "cred-id");
  assert.ok(json.response.attestationObject);
  assert.deepEqual(json.response.transports, ["internal"]);
  assert.equal(json.response.authenticatorData, undefined);
});

test("credentialToJSON: ログイン由来(authenticatorDataを持つ)credentialを正しい形状に変換する", () => {
  const json = credentialToJSON(fakeCredential({ isRegistration: false }));
  assert.ok(json.response.authenticatorData);
  assert.ok(json.response.signature);
  assert.ok(json.response.userHandle);
  assert.equal(json.response.attestationObject, undefined);
});

test("credentialToJSON: userHandleが無い(令 discoverable以外の一部認証器)場合はnullにする", () => {
  const credential = fakeCredential({ isRegistration: false });
  credential.response.userHandle = null;
  const json = credentialToJSON(credential);
  assert.equal(json.response.userHandle, null);
});

test("loginWithPasskey: 成功時はokになり、finishへ正しいstate/credentialを送る", async () => {
  const calls = [];
  const fetchImpl = async (url, init) => {
    calls.push({ url, init });
    if (url.endsWith("/login/begin")) {
      return fakeResponse({
        ok: true,
        status: 200,
        body: { state: "state-123", options: { challenge: "AQID", allowCredentials: [] } },
      });
    }
    return fakeResponse({ ok: true, status: 200, body: {} });
  };
  const credentialsApi = { get: async () => fakeCredential({ isRegistration: false }) };

  const result = await loginWithPasskey({ origin: "http://bff-rails", fetchImpl, credentialsApi });

  assert.deepEqual(result, { ok: true });
  assert.equal(calls.length, 2);
  const finishBody = JSON.parse(calls[1].init.body);
  assert.equal(finishBody.state, "state-123");
  assert.equal(finishBody.credential.id, "cred-id");
});

test("loginWithPasskey: beginが失敗(200以外)した場合、navigator.credentials.getを呼ばずに失敗を返す", async () => {
  let getCalled = false;
  const fetchImpl = async () => fakeResponse({ ok: false, status: 500, body: {} });
  const credentialsApi = { get: async () => { getCalled = true; return fakeCredential({ isRegistration: false }); } };

  const result = await loginWithPasskey({ origin: "http://bff-rails", fetchImpl, credentialsApi });

  assert.equal(result.ok, false);
  assert.equal(result.reason, "begin_failed");
  assert.equal(getCalled, false, "beginが失敗した時点でget()を呼んではいけない");
});

test("loginWithPasskey: finishが失敗した場合、backendのerrorメッセージを含めて返す", async () => {
  const fetchImpl = async (url) => {
    if (url.endsWith("/login/begin")) {
      return fakeResponse({ ok: true, status: 200, body: { state: "s", options: { challenge: "AQID" } } });
    }
    return fakeResponse({ ok: false, status: 422, body: { error: "signature_invalid" } });
  };
  const credentialsApi = { get: async () => fakeCredential({ isRegistration: false }) };

  const result = await loginWithPasskey({ origin: "http://bff-rails", fetchImpl, credentialsApi });

  assert.equal(result.ok, false);
  assert.equal(result.reason, "finish_failed");
  assert.equal(result.error, "signature_invalid");
});

test("loginWithPasskey: navigator.credentials.get自体が例外を投げても(ユーザーがダイアログをキャンセルした場合等)クラッシュせず失敗を返す", async () => {
  const fetchImpl = async () =>
    fakeResponse({ ok: true, status: 200, body: { state: "s", options: { challenge: "AQID" } } });
  const credentialsApi = { get: async () => { throw new DOMException("cancelled", "NotAllowedError"); } };

  const result = await loginWithPasskey({ origin: "http://bff-rails", fetchImpl, credentialsApi });

  assert.equal(result.ok, false);
  assert.equal(result.reason, "exception");
});

test("registerPasskeyCeremony: 未ログイン(401)の場合、専用のreasonを返しcreateを呼ばない", async () => {
  let createCalled = false;
  const fetchImpl = async () => fakeResponse({ ok: false, status: 401, body: {} });
  const credentialsApi = { create: async () => { createCalled = true; return fakeCredential({ isRegistration: true }); } };

  const result = await registerPasskeyCeremony({ origin: "http://bff-rails", fetchImpl, credentialsApi });

  assert.deepEqual(result, { ok: false, reason: "unauthenticated" });
  assert.equal(createCalled, false);
});

test("registerPasskeyCeremony: 成功時はokになり、user.idもchallengeもArrayBufferへ変換してcreateへ渡す", async () => {
  let passedOptions;
  const fetchImpl = async (url) => {
    if (url.endsWith("/register/begin")) {
      return fakeResponse({
        ok: true,
        status: 200,
        body: { state: "s2", options: { challenge: "AQID", user: { id: "BAUG", name: "u" } } },
      });
    }
    return fakeResponse({ ok: true, status: 200, body: {} });
  };
  const credentialsApi = {
    create: async ({ publicKey }) => {
      passedOptions = publicKey;
      return fakeCredential({ isRegistration: true });
    },
  };

  const result = await registerPasskeyCeremony({ origin: "http://bff-rails", fetchImpl, credentialsApi });

  assert.deepEqual(result, { ok: true });
  assert.ok(passedOptions.challenge instanceof ArrayBuffer, "challengeはArrayBufferに変換されているべき");
  assert.ok(passedOptions.user.id instanceof ArrayBuffer, "user.idもArrayBufferに変換されているべき");
});

test("registerPasskeyCeremony: finishが失敗した場合、backendのerrorメッセージを含めて返す", async () => {
  const fetchImpl = async (url) => {
    if (url.endsWith("/register/begin")) {
      return fakeResponse({
        ok: true,
        status: 200,
        body: { state: "s", options: { challenge: "AQID", user: { id: "BAUG" } } },
      });
    }
    return fakeResponse({ ok: false, status: 422, body: { error: "credential_id_taken" } });
  };
  const credentialsApi = { create: async () => fakeCredential({ isRegistration: true }) };

  const result = await registerPasskeyCeremony({ origin: "http://bff-rails", fetchImpl, credentialsApi });

  assert.equal(result.ok, false);
  assert.equal(result.reason, "finish_failed");
  assert.equal(result.error, "credential_id_taken");
});
