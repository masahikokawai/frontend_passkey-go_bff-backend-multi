import { apiFetch, ApiError } from "../src/shared/api/client";

// src/shared/api/client.test.ts (Vitest版)と同じ内容のJest版
// vi.stubGlobal ⇔ 素朴に global.fetch へ代入、という書き味の違いが分かる
//
// 【注記】jest-environment-jsdom はバージョンによって`window.location`の
// configurable さが異なる(Jest 30系に一時同梱されていたjsdomではlocationが
// 非configurableになり、丸ごとの差し替えができなかった)
// このプロジェクトは Jest 29系(jest-environment-jsdom 29)を使っており、
// Vitest同様に definePropertyでの丸ごと差し替えが可能
function stubLocation(pathname: string, search: string) {
  const location = { pathname, search, href: "" };
  Object.defineProperty(window, "location", {
    value: location,
    writable: true,
    configurable: true,
  });
  return location;
}

describe("apiFetch", () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("状態変更メソッド(POST等)でcsrf_token Cookieがあればヘッダに複製する", async () => {
    document.cookie = "csrf_token=abc123";
    const fetchMock = jest
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    global.fetch = fetchMock as unknown as typeof fetch;

    await apiFetch("/api/tasks", { method: "POST", body: JSON.stringify({}) });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [, init] = fetchMock.mock.calls[0];
    const headers = new Headers(init.headers);
    expect(headers.get("X-CSRF-Token")).toBe("abc123");
  });

  it("GETのような非状態変更メソッドではcsrf_token Cookieがあってもヘッダを付けない", async () => {
    document.cookie = "csrf_token=abc123";
    const fetchMock = jest
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    global.fetch = fetchMock as unknown as typeof fetch;

    await apiFetch("/api/tasks");

    const [, init] = fetchMock.mock.calls[0];
    const headers = new Headers(init.headers);
    expect(headers.get("X-CSRF-Token")).toBeNull();
  });

  it("401の場合は現在のpath+searchを付けてログイン画面へリダイレクトし、ApiErrorをthrowする", async () => {
    const location = stubLocation("/tasks", "?foo=bar");
    const fetchMock = jest.fn().mockResolvedValue(new Response(null, { status: 401 }));
    global.fetch = fetchMock as unknown as typeof fetch;

    await expect(apiFetch("/api/tasks")).rejects.toThrow(ApiError);
    expect(location.href).toBe(`/login?redirect=${encodeURIComponent("/tasks?foo=bar")}`);
  });

  it("401以外のエラー(500)はApiErrorをthrowするだけでリダイレクトは発生しない", async () => {
    const location = stubLocation("/tasks", "");
    const fetchMock = jest.fn().mockResolvedValue(new Response("server error", { status: 500 }));
    global.fetch = fetchMock as unknown as typeof fetch;

    await expect(apiFetch("/api/tasks")).rejects.toThrow(ApiError);
    expect(location.href).toBe("");
  });
});
