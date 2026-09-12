import { registerPasskey, loginWithPasskey, PasskeyUnsupportedError } from "../src/shared/webauthn/passkey";
import { apiFetch } from "../src/shared/api/client";

// src/shared/webauthn/passkey.test.ts (Vitest版)と同じ内容のJest版
// vi.mock/vi.stubGlobal ⇔ jest.mock/globalへの素朴な代入、という書き味の違いが分かる
// (client.test.ts・LoginForm.test.tsxと同じ手法をここでも踏襲する)
jest.mock("../src/shared/api/client", () => ({ apiFetch: jest.fn() }));

function stubWebauthnSupport() {
  const parseCreationOptionsFromJSON = jest.fn((json: unknown) => json);
  const parseRequestOptionsFromJSON = jest.fn((json: unknown) => json);
  Object.defineProperty(window, "PublicKeyCredential", {
    value: { parseCreationOptionsFromJSON, parseRequestOptionsFromJSON },
    configurable: true,
    writable: true,
  });
}

function restoreGlobals() {
  // @ts-expect-error テスト用にdeleteして次のテストへ影響させない
  delete window.PublicKeyCredential;
  // navigatorをテストごとに差し替えているため、jsdom既定へは戻さず単に削除する
  delete (global as { navigator?: unknown }).navigator;
}

describe("registerPasskey", () => {
  const apiFetchMock = apiFetch as jest.MockedFunction<typeof apiFetch>;

  beforeEach(() => {
    apiFetchMock.mockReset();
    stubWebauthnSupport();
  });
  afterEach(restoreGlobals);

  it("ブラウザがWebAuthn未対応の場合はPasskeyUnsupportedErrorを投げ、apiFetchを一切呼ばない", async () => {
    // @ts-expect-error 意図的に未対応ブラウザを再現する
    delete window.PublicKeyCredential;
    await expect(registerPasskey()).rejects.toThrow(PasskeyUnsupportedError);
    expect(apiFetchMock).not.toHaveBeenCalled();
  });

  it("端末名を指定すると、finishのURLにクエリとしてエンコードされて渡る", async () => {
    apiFetchMock.mockResolvedValueOnce({ publicKey: { challenge: "c" } } as never);
    apiFetchMock.mockResolvedValueOnce(undefined as never);
    const toJSON = jest.fn(() => ({ id: "cred-1" }));
    (global as { navigator?: unknown }).navigator = {
      credentials: { create: jest.fn().mockResolvedValue({ toJSON }) },
    };

    await registerPasskey("MacBook Pro");

    expect(apiFetchMock).toHaveBeenNthCalledWith(1, "/api/auth/passkey/register/begin", { method: "POST" });
    expect(apiFetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/auth/passkey/register/finish?name=MacBook%20Pro",
      { method: "POST", body: JSON.stringify({ id: "cred-1" }) }
    );
  });

  it("端末名を省略した場合、finishのURLにクエリを付けない", async () => {
    apiFetchMock.mockResolvedValueOnce({ publicKey: { challenge: "c" } } as never);
    apiFetchMock.mockResolvedValueOnce(undefined as never);
    (global as { navigator?: unknown }).navigator = {
      credentials: { create: jest.fn().mockResolvedValue({ toJSON: () => ({}) }) },
    };

    await registerPasskey();

    expect(apiFetchMock).toHaveBeenNthCalledWith(2, "/api/auth/passkey/register/finish", expect.anything());
  });

  it("navigator.credentials.create()がnullを返す場合はエラーを投げ、finishは呼ばない", async () => {
    apiFetchMock.mockResolvedValueOnce({ publicKey: { challenge: "c" } } as never);
    (global as { navigator?: unknown }).navigator = {
      credentials: { create: jest.fn().mockResolvedValue(null) },
    };

    await expect(registerPasskey()).rejects.toThrow("パスキーの作成がキャンセルされました");
    expect(apiFetchMock).toHaveBeenCalledTimes(1);
  });
});

describe("loginWithPasskey", () => {
  beforeEach(() => {
    stubWebauthnSupport();
  });
  afterEach(restoreGlobals);

  it("ブラウザがWebAuthn未対応の場合はPasskeyUnsupportedErrorを投げる", async () => {
    // @ts-expect-error 意図的に未対応ブラウザを再現する
    delete window.PublicKeyCredential;
    await expect(loginWithPasskey()).rejects.toThrow(PasskeyUnsupportedError);
  });

  it("begin呼び出しが失敗(ok=false)した場合、専用のエラーメッセージを投げる", async () => {
    global.fetch = jest.fn().mockResolvedValue({ ok: false, json: async () => ({}) }) as unknown as typeof fetch;
    await expect(loginWithPasskey()).rejects.toThrow("パスキーログインの開始に失敗しました");
  });

  it("navigator.credentials.get()がnullを返す場合、finishを呼ばずにエラーを投げる", async () => {
    const fetchMock = jest
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ publicKey: { challenge: "c" }, state: "s" }) });
    global.fetch = fetchMock as unknown as typeof fetch;
    (global as { navigator?: unknown }).navigator = { credentials: { get: jest.fn().mockResolvedValue(null) } };

    await expect(loginWithPasskey()).rejects.toThrow("パスキーでのログインがキャンセルされました");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("finish呼び出しが失敗(ok=false)した場合、専用のエラーメッセージを投げる", async () => {
    const fetchMock = jest
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ publicKey: { challenge: "c" }, state: "s" }) })
      .mockResolvedValueOnce({ ok: false });
    global.fetch = fetchMock as unknown as typeof fetch;
    (global as { navigator?: unknown }).navigator = {
      credentials: { get: jest.fn().mockResolvedValue({ toJSON: () => ({ id: "cred-1" }) }) },
    };

    await expect(loginWithPasskey()).rejects.toThrow("パスキーの検証に失敗しました");
  });

  it("正常系: stateがfinishのクエリにエンコードされて渡り、両呼び出しともcredentials: includeを指定する", async () => {
    const fetchMock = jest
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ publicKey: { challenge: "c" }, state: "state with space" }),
      })
      .mockResolvedValueOnce({ ok: true });
    global.fetch = fetchMock as unknown as typeof fetch;
    (global as { navigator?: unknown }).navigator = {
      credentials: { get: jest.fn().mockResolvedValue({ toJSON: () => ({ id: "cred-1" }) }) },
    };

    await loginWithPasskey();

    expect(fetchMock.mock.calls[1][0]).toBe("/api/auth/passkey/login/finish?state=state%20with%20space");
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: "include" });
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ credentials: "include" });
  });
});
