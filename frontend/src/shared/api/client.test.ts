import { describe, it, expect, vi, afterEach } from "vitest";
import { apiFetch, ApiError } from "./client";

// window.locationはjsdomでは直接代入できない(実際のnavigationを試みてしまう)ため、
// テスト用の素朴なオブジェクトに丸ごと差し替える
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
    vi.unstubAllGlobals();
  });

  it("状態変更メソッド(POST等)でcsrf_token Cookieがあればヘッダに複製する", async () => {
    document.cookie = "csrf_token=abc123";
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await apiFetch("/api/tasks", { method: "POST", body: JSON.stringify({}) });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [, init] = fetchMock.mock.calls[0];
    const headers = new Headers(init.headers);
    expect(headers.get("X-CSRF-Token")).toBe("abc123");
  });

  it("GETのような非状態変更メソッドではcsrf_token Cookieがあってもヘッダを付けない", async () => {
    document.cookie = "csrf_token=abc123";
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await apiFetch("/api/tasks");

    const [, init] = fetchMock.mock.calls[0];
    const headers = new Headers(init.headers);
    expect(headers.get("X-CSRF-Token")).toBeNull();
  });

  it("401の場合は現在のpath+searchを付けてログイン画面へリダイレクトし、ApiErrorをthrowする", async () => {
    const location = stubLocation("/tasks", "?foo=bar");
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 401 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiFetch("/api/tasks")).rejects.toThrow(ApiError);
    expect(location.href).toBe(`/login?redirect=${encodeURIComponent("/tasks?foo=bar")}`);
  });

  it("401以外のエラー(500)はApiErrorをthrowするだけでリダイレクトは発生しない", async () => {
    const location = stubLocation("/tasks", "");
    const fetchMock = vi.fn().mockResolvedValue(new Response("server error", { status: 500 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiFetch("/api/tasks")).rejects.toThrow(ApiError);
    expect(location.href).toBe("");
  });

  // 【テスト監査で発見・修正】backendのエラーレスポンスは{"error":"validation_error","message":"..."}
  // というJSON形状(backend/internal/handler/v1/render.goのrenderServiceError参照)だが、
  // 以前はここでJSON.parseしておらず、生のJSON文字列全体がApiError.messageになっていた。
  // 各画面はerr.messageをそのまま表示するため、修正前はユーザーに
  // 生の{"error":"validation_error","message":"..."}という文字列が見えていた
  it("エラーレスポンスのJSONボディから message フィールドを抽出する(生のJSON文字列を表示しない)", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: "validation_error", message: "nameは20文字以内である必要があります" }), {
        status: 422,
      })
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiFetch("/api/tasks")).rejects.toMatchObject({
      message: "nameは20文字以内である必要があります",
    });
  });

  it("エラーレスポンスのJSONボディにmessageが無い場合はerrorフィールドを使う", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "not_found" }), { status: 404 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiFetch("/api/tasks/999")).rejects.toMatchObject({ message: "not_found" });
  });

  it("エラーレスポンスがJSONでない場合(HTMLエラーページ等)は本文をそのままメッセージにする", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("<html><body>502 Bad Gateway</body></html>", {
        status: 502,
        headers: { "Content-Type": "text/html" },
      })
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiFetch("/api/tasks")).rejects.toMatchObject({
      message: "<html><body>502 Bad Gateway</body></html>",
    });
  });

  // 【テスト監査で発見・修正】以前はresponse.json()を直接呼んでおり、200応答でも
  // ボディがJSONとして解析できない場合、ブラウザ組み込みのSyntaxErrorがそのまま
  // 各画面まで伝播し、「一覧の取得に失敗しました」等の代わりに意味不明なJSエラー文言が出ていた
  it("200応答でもボディがJSONとして解析できない場合はApiErrorをthrowする(SyntaxErrorを漏らさない)", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("<html>not json</html>", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiFetch("/api/tasks")).rejects.toThrow(ApiError);
  });
});
