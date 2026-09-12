import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import Layout from "./Layout";
import { apiFetch } from "@/shared/api/client";

vi.mock("@/shared/api/client", () => ({
  apiFetch: vi.fn(),
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
    vi.mocked(apiFetch).mockReset();
  });

  it("ログアウトボタン押下でPOST /api/auth/logoutを呼び、戻り値のredirectUrlへ遷移する", async () => {
    vi.mocked(apiFetch).mockResolvedValue({
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
    vi.mocked(apiFetch).mockRejectedValue(new Error("ログアウトできません"));
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
