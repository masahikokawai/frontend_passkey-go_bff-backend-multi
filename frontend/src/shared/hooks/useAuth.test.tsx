import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { useAuth } from "./useAuth";

// useAuthはグローバルfetch(apiFetch経由)にしか依存しないため、Routerの文脈は不要
function Probe() {
  const auth = useAuth();
  return <div data-testid="probe">{auth.status}</div>;
}

const originalLocation = window.location;

function stubLocation() {
  const stub = { ...originalLocation, href: "", pathname: "/tasks", search: "" };
  delete (window as { location?: unknown }).location;
  // @ts-expect-error jsdomのLocation型は本来readonlyだが、テスト用に丸ごと差し替える
  window.location = stub;
}

afterEach(() => {
  delete (window as { location?: unknown }).location;
  // @ts-expect-error stubLocationと対になる復元処理
  window.location = originalLocation;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("useAuth", () => {
  it("/api/meが200を返せばauthenticatedになりレスポンスを保持する", async () => {
    stubLocation();
    const me = { user: { id: 1, name: "太郎", email: "a@example.com", role: "general" }, feature_flags: {} };
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => me })
    );

    render(<Probe />);
    expect(screen.getByTestId("probe")).toHaveTextContent("loading");
    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("authenticated"));
  });

  it("/api/meが401を返せばunauthenticatedになる(apiFetch自体が/loginへ誘導する)", async () => {
    stubLocation();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, status: 401, text: async () => "" })
    );

    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("unauthenticated"));
  });

  it("/api/meが500(401以外のエラー)を返してもunauthenticated扱いにしてログイン導線を出す", async () => {
    stubLocation();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, status: 500, text: async () => "internal error" })
    );

    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("unauthenticated"));
    // 401ではないため、apiFetch自身の/loginリダイレクトは発生しない
    // (useAuth側の判断だけでunauthenticatedにフォールバックしていることの確認)
    expect(window.location.href).toBe("");
  });

  it("fetch自体が失敗(ネットワークエラー)してもunauthenticated扱いにする", async () => {
    stubLocation();
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("unauthenticated"));
  });
});
