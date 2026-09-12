import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import LoginForm from "../src/features/auth/LoginForm";
import { PasskeyUnsupportedError } from "../src/shared/webauthn/passkey";

// CONTRACT.mdセクション22.6: navigator.credentials自体はjsdomに実装が無いため、
// ブラウザAPIを直接呼ぶshared/webauthn/passkey.tsをモックする(Vitest版のvi.mockに相当)
const loginWithPasskeyMock = jest.fn();
jest.mock("../src/shared/webauthn/passkey", () => ({
  ...jest.requireActual("../src/shared/webauthn/passkey"),
  loginWithPasskey: () => loginWithPasskeyMock(),
}));

// src/features/auth/LoginForm.test.tsx (Vitest版)と同じ内容のJest版
// vi.stubGlobal/vi.mock ⇔ global.fetchへの素朴な代入・Object.definePropertyでの
// window.location差し替え、という書き味の違いが分かる(client.test.tsと同じ手法)
function stubLocation(search = "") {
  const location = { href: "", search };
  Object.defineProperty(window, "location", {
    value: location,
    writable: true,
    configurable: true,
  });
  return location;
}

describe("LoginForm", () => {
  afterEach(() => {
    jest.restoreAllMocks();
    loginWithPasskeyMock.mockReset();
  });

  it("ログイン成功時、レスポンスのredirect先へ遷移する", async () => {
    const location = stubLocation();
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ redirect: "/tasks" }),
    }) as unknown as typeof fetch;

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    await waitFor(() => expect(location.href).toBe("/tasks"));
    expect(fetch).toHaveBeenCalledWith(
      "/api/auth/login",
      expect.objectContaining({ method: "POST", credentials: "include" })
    );
  });

  it("invalid_credentialsの場合、専用のエラーメッセージを表示する", async () => {
    stubLocation();
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      json: async () => ({ error: "invalid_credentials" }),
    }) as unknown as typeof fetch;

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "wrong@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "wrong");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    expect(await screen.findByTestId("login-error")).toHaveTextContent(
      "メールアドレスまたはパスワードが正しくありません"
    );
  });

  it("password_expiredの場合、有効期限切れのエラーメッセージを表示する", async () => {
    stubLocation();
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      json: async () => ({ error: "password_expired" }),
    }) as unknown as typeof fetch;

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "expired");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    expect(await screen.findByTestId("login-error")).toHaveTextContent(
      "パスワードの有効期限が切れています"
    );
  });

  it("HMAC版画面ではRSA版へのリンクを表示する", () => {
    stubLocation();
    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    expect(screen.getByTestId("login-rsa-link")).toHaveAttribute("href", "/login/rsa");
  });

  it("RSA版画面ではHMAC版へのリンクを表示する", () => {
    stubLocation();
    render(<LoginForm loginPath="/api/auth/login/rsa" variantLabel="RSA版" />);
    expect(screen.getByTestId("login-hmac-link")).toHaveAttribute("href", "/login");
  });

  it("Keycloakボタンをクリックすると /api/auth/login/keycloak へ遷移する", async () => {
    const location = stubLocation();
    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-keycloak-button"));
    expect(location.href).toBe("/api/auth/login/keycloak?redirect=%2Ftasks");
  });

  it("未知のエラーコードの場合、汎用のエラーメッセージを表示する(ERROR_MESSAGESに無いキー)", async () => {
    stubLocation();
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      json: async () => ({ error: "some_unmapped_error" }),
    }) as unknown as typeof fetch;

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    expect(await screen.findByTestId("login-error")).toHaveTextContent("ログインに失敗しました");
  });

  it("fetch自体が失敗(ネットワークエラー)した場合も、汎用のエラーメッセージを表示し送信ボタンを再度押せる状態に戻す", async () => {
    stubLocation();
    global.fetch = jest.fn().mockRejectedValue(new Error("network down")) as unknown as typeof fetch;

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    expect(await screen.findByTestId("login-error")).toHaveTextContent("ログインに失敗しました");
    expect(screen.getByTestId("login-submit-button")).not.toBeDisabled();
  });

  it("成功レスポンスのredirectが空の場合、URLの?redirectクエリへフォールバックする", async () => {
    const location = stubLocation("?redirect=%2Flabels");
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ redirect: "" }),
    }) as unknown as typeof fetch;

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    await waitFor(() => expect(location.href).toBe("/labels"));
  });

  it("送信中は送信ボタンがdisabledになり、二重送信を防ぐ", async () => {
    const location = stubLocation();
    let resolveFetch: (value: { ok: boolean; json: () => Promise<unknown> }) => void;
    global.fetch = jest.fn(
      () =>
        new Promise((resolve) => {
          resolveFetch = resolve;
        })
    ) as unknown as typeof fetch;

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    const submitButton = screen.getByTestId("login-submit-button");
    expect(submitButton).not.toBeDisabled();

    await userEvent.click(submitButton);
    await waitFor(() => expect(submitButton).toBeDisabled());

    resolveFetch!({ ok: true, json: async () => ({ redirect: "/tasks" }) });
    await waitFor(() => expect(location.href).toBe("/tasks"));
  });

  it("パスキーでログインに成功すると、redirect先へ遷移する", async () => {
    const location = stubLocation();
    loginWithPasskeyMock.mockResolvedValue(undefined);

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-passkey-button"));

    await waitFor(() => expect(location.href).toBe("/tasks"));
    expect(loginWithPasskeyMock).toHaveBeenCalled();
  });

  it("パスキーでのログインに失敗すると、エラーメッセージを表示する", async () => {
    stubLocation();
    loginWithPasskeyMock.mockRejectedValue(new Error("failed"));

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-passkey-button"));

    expect(await screen.findByTestId("login-error")).toHaveTextContent(
      "パスキーでのログインに失敗しました"
    );
  });

  it("パスキー未対応ブラウザの場合、専用のエラーメッセージを表示する", async () => {
    stubLocation();
    loginWithPasskeyMock.mockRejectedValue(new PasskeyUnsupportedError());

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-passkey-button"));

    expect(await screen.findByTestId("login-error")).toHaveTextContent(
      "このブラウザはパスキー"
    );
  });

  // src/features/auth/LoginForm.test.tsx (Vitest版)と同じ内容のJest版
  it("パスキーログイン待ちの間にアンマウントされても、Reactの状態更新警告が出ない", async () => {
    stubLocation();
    const consoleError = jest.spyOn(console, "error").mockImplementation(() => {});
    let rejectLogin: (err: Error) => void = () => {};
    loginWithPasskeyMock.mockReturnValue(
      new Promise((_resolve, reject) => {
        rejectLogin = reject;
      })
    );

    const { unmount } = render(
      <LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />
    );
    await userEvent.click(screen.getByTestId("login-passkey-button"));
    unmount();

    rejectLogin(new Error("too late"));
    await new Promise((r) => setTimeout(r, 0));

    const gotUnmountWarning = consoleError.mock.calls.some((call) =>
      String(call[0]).includes("state update on an unmounted component")
    );
    expect(gotUnmountWarning).toBe(false);
    consoleError.mockRestore();
  });

  // src/features/auth/LoginForm.test.tsx (Vitest版)と同じ内容のJest版
  // 【3回目のテスト監査(セキュリティ)で発見・修正】?redirect=クエリパラメータに
  // 外部ドメインを指すような値を渡すと、パスキーログイン成功後にbffを一切経由せず
  // フロントエンド単体でそのドメインへ遷移してしまうOpen Redirect脆弱性があった
  it("パスキーログイン成功時、?redirectに外部ドメインを指定しても遷移先はデフォルトのままになる(Open Redirect対策)", async () => {
    const location = stubLocation("?redirect=http%3A%2F%2Fevil.com");
    loginWithPasskeyMock.mockResolvedValue(undefined);

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-passkey-button"));

    await waitFor(() => expect(location.href).toBe("/tasks"));
  });

  it("パスキーログイン成功時、?redirectに@evil.comのようなuserinfo混入パターンを指定しても遷移先はデフォルトのままになる", async () => {
    const location = stubLocation("?redirect=%40evil.com%2Fphish");
    loginWithPasskeyMock.mockResolvedValue(undefined);

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-passkey-button"));

    await waitFor(() => expect(location.href).toBe("/tasks"));
  });

  it("Keycloakボタンをクリックする際も、?redirectに不正な値があればbffへ渡すクエリはデフォルトのパスに正規化される", async () => {
    const location = stubLocation("?redirect=%2F%2Fevil.com");

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-keycloak-button"));

    expect(location.href).toBe("/api/auth/login/keycloak?redirect=%2Ftasks");
  });

  it("?redirectが安全な相対パスの場合は、そのままKeycloakへのクエリに使われる", async () => {
    const location = stubLocation("?redirect=%2Flabels");

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-keycloak-button"));

    expect(location.href).toBe("/api/auth/login/keycloak?redirect=%2Flabels");
  });
});
