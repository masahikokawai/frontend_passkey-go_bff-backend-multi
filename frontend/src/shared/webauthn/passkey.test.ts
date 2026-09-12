import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { registerPasskey, loginWithPasskey, PasskeyUnsupportedError } from "./passkey";
import { apiFetch } from "@/shared/api/client";

// registerPasskeyはapiFetch(セッションCookie/CSRF自動付与)を使うためモックする loginWithPasskey は未ログイン状態から呼ばれる
// (apiFetch は401を検知して即/loginへリダイレクトしてしまうため使えない、passkey.ts 本体のコメント参照)
// ので、こちらはグローバルfetchを直接モックする
// 両者で検証方法を変える必要がある点自体がこのテストで押さえたいポイント
vi.mock("@/shared/api/client", () => ({ apiFetch: vi.fn() }));

// jsdom は既定で WebAuthn 関連のグローバル(PublicKeyCredential・navigator.credentials)を持たないため、テストごとに最小限のスタブを用意する
// 存在しない場合の分岐(assertSupported)を確認するテストでは、あえてこのスタブを外す
function stubWebauthnSupport() {
  const parseCreationOptionsFromJSON = vi.fn((json: unknown) => json);
  const parseRequestOptionsFromJSON = vi.fn((json: unknown) => json);
  Object.defineProperty(window, "PublicKeyCredential", {
    value: { parseCreationOptionsFromJSON, parseRequestOptionsFromJSON },
    configurable: true,
    writable: true,
  });
  return { parseCreationOptionsFromJSON, parseRequestOptionsFromJSON };
}

describe("registerPasskey", () => {
  beforeEach(() => {
    vi.mocked(apiFetch).mockReset();
    stubWebauthnSupport();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    // @ts-expect-error テスト用にdeleteして次のテストへ影響させない
    delete window.PublicKeyCredential;
  });

  it("ブラウザがWebAuthn未対応の場合はPasskeyUnsupportedErrorを投げ、apiFetchを一切呼ばない", async () => {
    // @ts-expect-error 意図的に未対応ブラウザを再現する
    delete window.PublicKeyCredential;
    await expect(registerPasskey()).rejects.toThrow(PasskeyUnsupportedError);
    expect(apiFetch).not.toHaveBeenCalled();
  });

  it("端末名を指定すると、finishのURLにクエリとしてエンコードされて渡る", async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce({ publicKey: { challenge: "c" } });
    vi.mocked(apiFetch).mockResolvedValueOnce(undefined);
    const toJSON = vi.fn(() => ({ id: "cred-1" }));
    vi.stubGlobal("navigator", {
      credentials: { create: vi.fn().mockResolvedValue({ toJSON }) },
    });

    await registerPasskey("MacBook Pro");

    expect(apiFetch).toHaveBeenNthCalledWith(1, "/api/auth/passkey/register/begin", { method: "POST" });
    expect(apiFetch).toHaveBeenNthCalledWith(
      2,
      "/api/auth/passkey/register/finish?name=MacBook%20Pro",
      { method: "POST", body: JSON.stringify({ id: "cred-1" }) }
    );
  });

  it("端末名を省略した場合、finishのURLにクエリを付けない", async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce({ publicKey: { challenge: "c" } });
    vi.mocked(apiFetch).mockResolvedValueOnce(undefined);
    vi.stubGlobal("navigator", {
      credentials: { create: vi.fn().mockResolvedValue({ toJSON: () => ({}) }) },
    });

    await registerPasskey();

    expect(apiFetch).toHaveBeenNthCalledWith(2, "/api/auth/passkey/register/finish", expect.anything());
  });

  it("navigator.credentials.create()がnullを返す(ユーザーがキャンセル等)場合はエラーを投げ、finishは呼ばない", async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce({ publicKey: { challenge: "c" } });
    vi.stubGlobal("navigator", { credentials: { create: vi.fn().mockResolvedValue(null) } });

    await expect(registerPasskey()).rejects.toThrow("パスキーの作成がキャンセルされました");
    expect(apiFetch).toHaveBeenCalledTimes(1);
  });
});

describe("loginWithPasskey", () => {
  beforeEach(() => {
    stubWebauthnSupport();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    // @ts-expect-error テスト用にdeleteして次のテストへ影響させない
    delete window.PublicKeyCredential;
  });

  it("ブラウザがWebAuthn未対応の場合はPasskeyUnsupportedErrorを投げる", async () => {
    // @ts-expect-error 意図的に未対応ブラウザを再現する
    delete window.PublicKeyCredential;
    await expect(loginWithPasskey()).rejects.toThrow(PasskeyUnsupportedError);
  });

  it("begin呼び出しが失敗(ok=false)した場合、専用のエラーメッセージを投げる", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) })
    );
    await expect(loginWithPasskey()).rejects.toThrow("パスキーログインの開始に失敗しました");
  });

  it("navigator.credentials.get()がnullを返す場合、finishを呼ばずにエラーを投げる", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ publicKey: { challenge: "c" }, state: "s" }) });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("navigator", { credentials: { get: vi.fn().mockResolvedValue(null) } });

    await expect(loginWithPasskey()).rejects.toThrow("パスキーでのログインがキャンセルされました");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("finish呼び出しが失敗(ok=false)した場合、専用のエラーメッセージを投げる", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ publicKey: { challenge: "c" }, state: "s" }) })
      .mockResolvedValueOnce({ ok: false });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("navigator", {
      credentials: { get: vi.fn().mockResolvedValue({ toJSON: () => ({ id: "cred-1" }) }) },
    });

    await expect(loginWithPasskey()).rejects.toThrow("パスキーの検証に失敗しました");
  });

  it("正常系: state(discoverable credential用の一時トークン)がfinishのクエリにエンコードされて渡る", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ publicKey: { challenge: "c" }, state: "state with space" }),
      })
      .mockResolvedValueOnce({ ok: true });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("navigator", {
      credentials: { get: vi.fn().mockResolvedValue({ toJSON: () => ({ id: "cred-1" }) }) },
    });

    await loginWithPasskey();

    const secondCallURL = fetchMock.mock.calls[1][0] as string;
    expect(secondCallURL).toBe("/api/auth/passkey/login/finish?state=state%20with%20space");
    // credentials: "include" を両方の呼び出しで指定していること
    // (セッションCookie を未ログイン状態でも送受信できるようにするため
    // apiFetch ではなく素の fetch を使っている以上、
    // ここを忘れるとログイン成功後のCookieが保存されない)
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: "include" });
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ credentials: "include" });
  });
});
