import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import Layout from "../src/shared/components/Layout";
import { apiFetch } from "../src/shared/api/client";

// src/shared/components/Layout.test.tsx (Vitest版)と同じ内容のJest版
jest.mock("../src/shared/api/client", () => ({
  __esModule: true,
  apiFetch: jest.fn(),
}));

function stubLocation() {
  const location = { href: "" };
  Object.defineProperty(window, "location", {
    value: location,
    writable: true,
    configurable: true,
  });
  return location;
}

describe("Layout", () => {
  beforeEach(() => {
    (apiFetch as jest.Mock).mockReset();
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("ログアウトボタン押下でPOST /api/auth/logoutを呼び、戻り値のredirectUrlへ遷移する", async () => {
    (apiFetch as jest.Mock).mockResolvedValue({
      redirectUrl: "https://keycloak.example.com/realms/training/protocol/openid-connect/logout?...",
    });
    const location = stubLocation();

    render(
      <Layout user={{ id: 1, name: "太郎", email: "taro@example.com", role: "general" }}>
        <div>content</div>
      </Layout>
    );

    await userEvent.click(screen.getByTestId("logout-button"));

    expect(apiFetch).toHaveBeenCalledWith("/api/auth/logout", { method: "POST" });
    expect(location.href).toBe(
      "https://keycloak.example.com/realms/training/protocol/openid-connect/logout?..."
    );
  });

  it("ログアウトAPIが失敗した場合、エラーメッセージが表示される(未処理のPromise rejectionにならない)", async () => {
    (apiFetch as jest.Mock).mockRejectedValue(new Error("ログアウトできません"));
    stubLocation();

    render(
      <Layout user={{ id: 1, name: "太郎", email: "taro@example.com", role: "general" }}>
        <div>content</div>
      </Layout>
    );

    await userEvent.click(screen.getByTestId("logout-button"));

    expect(await screen.findByText("ログアウトできません")).toBeInTheDocument();
  });
});
