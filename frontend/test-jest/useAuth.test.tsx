import { render, screen, waitFor } from "@testing-library/react";
import { useAuth } from "../src/shared/hooks/useAuth";

// src/shared/hooks/useAuth.test.tsx (Vitest版)と同じ内容のJest版
function Probe() {
  const auth = useAuth();
  return <div data-testid="probe">{auth.status}</div>;
}

const originalLocation = window.location;

function stubLocation() {
  const stub = { ...originalLocation, href: "", pathname: "/tasks", search: "" };
  // @ts-expect-error テスト用に丸ごと差し替える
  delete window.location;
  // @ts-expect-error jsdomのLocation型は本来readonlyだが、テスト用に丸ごと差し替える
  window.location = stub;
}

afterEach(() => {
  // @ts-expect-error stubLocationと対になる復元処理
  delete window.location;
  // @ts-expect-error 同上
  window.location = originalLocation;
  jest.restoreAllMocks();
  // @ts-expect-error globalThis.fetchのモックを剥がす(vi.unstubAllGlobals相当)
  delete global.fetch;
});

describe("useAuth", () => {
  it("/api/meが200を返せばauthenticatedになりレスポンスを保持する", async () => {
    stubLocation();
    const me = { user: { id: 1, name: "太郎", email: "a@example.com", role: "general" }, feature_flags: {} };
    global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200, json: async () => me }) as jest.Mock;

    render(<Probe />);
    expect(screen.getByTestId("probe")).toHaveTextContent("loading");
    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("authenticated"));
  });

  it("/api/meが401を返せばunauthenticatedになる(apiFetch自体が/loginへ誘導する)", async () => {
    stubLocation();
    global.fetch = jest.fn().mockResolvedValue({ ok: false, status: 401, text: async () => "" }) as jest.Mock;

    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("unauthenticated"));
  });

  it("/api/meが500(401以外のエラー)を返してもunauthenticated扱いにしてログイン導線を出す", async () => {
    stubLocation();
    global.fetch = jest.fn().mockResolvedValue({ ok: false, status: 500, text: async () => "internal error" }) as jest.Mock;

    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("unauthenticated"));
    expect(window.location.href).toBe("");
  });

  it("fetch自体が失敗(ネットワークエラー)してもunauthenticated扱いにする", async () => {
    stubLocation();
    global.fetch = jest.fn().mockRejectedValue(new TypeError("Failed to fetch")) as jest.Mock;

    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("unauthenticated"));
  });
});
