import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import LoginForm from "./LoginForm";
import { PasskeyUnsupportedError } from "@/shared/webauthn/passkey";

// CONTRACT.mdセクション22.6: navigator.credentials自体はjsdomに実装が無いため、
// ブラウザAPIを直接呼ぶshared/webauthn/passkey.tsをモックし、
// LoginFormからの呼び出し方(成功/失敗時の画面遷移・エラーメッセージ)だけを検証する
const { loginWithPasskeyMock } = vi.hoisted(() => ({ loginWithPasskeyMock: vi.fn() }));
vi.mock("@/shared/webauthn/passkey", async () => {
  const actual = await vi.importActual<typeof import("@/shared/webauthn/passkey")>(
    "@/shared/webauthn/passkey"
  );
  return { ...actual, loginWithPasskey: loginWithPasskeyMock };
});

const originalLocation = window.location;

function stubLocation() {
  // window.location.href への代入を検証できるよう、書き込み可能なスタブに差し替える
  // jsdomは既定でnavigationをサポートしないため("Not implemented: navigation")、
  // href setterだけを持つ簡易オブジェクトで置き換える
  const stub = { ...originalLocation, href: "", search: "" };
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
  loginWithPasskeyMock.mockReset();
});

describe("LoginForm", () => {
  it("ログイン成功時、レスポンスのredirect先へ遷移する", async () => {
    stubLocation();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ redirect: "/tasks" }),
      })
    );

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    await waitFor(() => expect(window.location.href).toBe("/tasks"));
    expect(fetch).toHaveBeenCalledWith(
      "/api/auth/login",
      expect.objectContaining({ method: "POST", credentials: "include" })
    );
  });

  it("invalid_credentialsの場合、専用のエラーメッセージを表示する", async () => {
    stubLocation();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        json: async () => ({ error: "invalid_credentials" }),
      })
    );

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
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        json: async () => ({ error: "password_expired" }),
      })
    );

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
    stubLocation();
    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-keycloak-button"));
    expect(window.location.href).toBe("/api/auth/login/keycloak?redirect=%2Ftasks");
  });

  it("未知のエラーコードの場合、汎用のエラーメッセージを表示する(ERROR_MESSAGESに無いキー)", async () => {
    stubLocation();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        json: async () => ({ error: "some_unmapped_error" }),
      })
    );

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    expect(await screen.findByTestId("login-error")).toHaveTextContent("ログインに失敗しました");
  });

  it("fetch自体が失敗(ネットワークエラー)した場合も、汎用のエラーメッセージを表示し送信ボタンを再度押せる状態に戻す", async () => {
    stubLocation();
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("network down")));

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    expect(await screen.findByTestId("login-error")).toHaveTextContent("ログインに失敗しました");
    expect(screen.getByTestId("login-submit-button")).not.toBeDisabled();
  });

  it("成功レスポンスのredirectが空の場合、URLの?redirectクエリへフォールバックする", async () => {
    stubLocation();
    window.location.search = "?redirect=%2Flabels";
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ redirect: "" }),
      })
    );

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    await userEvent.click(screen.getByTestId("login-submit-button"));

    await waitFor(() => expect(window.location.href).toBe("/labels"));
  });

  it("送信中は送信ボタンがdisabledになり、二重送信を防ぐ", async () => {
    stubLocation();
    let resolveFetch: (value: { ok: boolean; json: () => Promise<unknown> }) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        () =>
          new Promise((resolve) => {
            resolveFetch = resolve;
          })
      )
    );

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.type(screen.getByTestId("login-email-input"), "local-user@example.com");
    await userEvent.type(screen.getByTestId("login-password-input"), "password");
    const submitButton = screen.getByTestId("login-submit-button");
    expect(submitButton).not.toBeDisabled();

    await userEvent.click(submitButton);
    expect(submitButton).toBeDisabled();

    resolveFetch!({ ok: true, json: async () => ({ redirect: "/tasks" }) });
    await waitFor(() => expect(window.location.href).toBe("/tasks"));
  });

  it("パスキーでログインに成功すると、redirect先へ遷移する", async () => {
    stubLocation();
    loginWithPasskeyMock.mockResolvedValue(undefined);

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-passkey-button"));

    await waitFor(() => expect(window.location.href).toBe("/tasks"));
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

  // 【2回目のテスト監査で追加】navigator.credentials.get()はユーザー操作待ちで長時間
  // ブロックしうるため、待機中に別画面へ遷移してLoginFormがアンマウントされる余地がある
  // アンマウント後にsetState系を呼ぶとReactが
  // "Can't perform a state update on an unmounted component" 警告(console.error)を出す
  // このテストはその警告が出ないこと(=mountedRefガードが機能していること)を確認する
  it("パスキーログイン待ちの間にアンマウントされても、Reactの状態更新警告が出ない", async () => {
    stubLocation();
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
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

    // アンマウント後にpromiseがrejectされても、mountedRefガードによりsetStateは呼ばれない
    rejectLogin(new Error("too late"));
    await new Promise((r) => setTimeout(r, 0));

    const gotUnmountWarning = consoleError.mock.calls.some((call) =>
      String(call[0]).includes("state update on an unmounted component")
    );
    expect(gotUnmountWarning).toBe(false);
    consoleError.mockRestore();
  });

  // 【3回目のテスト監査(セキュリティ)で発見・修正】
  // ?redirect= クエリパラメータに外部ドメインを指すような値を渡すと、
  // パスキーログイン成功後に bff を一切経由せずフロントエンド単体でそのドメインへ遷移してしまう
  // Open Redirect の脆弱性があった
  // 実際に悪用可能だった入力パターンを、成功時の遷移先として使わせないことを確認する
  it("パスキーログイン成功時、?redirectに外部ドメインを指定しても遷移先はデフォルトのままになる(Open Redirect対策)", async () => {
    stubLocation();
    window.location.search = "?redirect=http%3A%2F%2Fevil.com";
    loginWithPasskeyMock.mockResolvedValue(undefined);

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-passkey-button"));

    await waitFor(() => expect(window.location.href).toBe("/tasks"));
  });

  it("パスキーログイン成功時、?redirectに@evil.comのようなuserinfo混入パターンを指定しても遷移先はデフォルトのままになる", async () => {
    stubLocation();
    window.location.search = "?redirect=%40evil.com%2Fphish";
    loginWithPasskeyMock.mockResolvedValue(undefined);

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-passkey-button"));

    await waitFor(() => expect(window.location.href).toBe("/tasks"));
  });

  it("Keycloakボタンをクリックする際も、?redirectに不正な値があればbffへ渡すクエリはデフォルトのパスに正規化される", async () => {
    stubLocation();
    window.location.search = "?redirect=%2F%2Fevil.com";

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-keycloak-button"));

    expect(window.location.href).toBe("/api/auth/login/keycloak?redirect=%2Ftasks");
  });

  it("?redirectが安全な相対パスの場合は、そのままKeycloakへのクエリに使われる", async () => {
    stubLocation();
    window.location.search = "?redirect=%2Flabels";

    render(<LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />);
    await userEvent.click(screen.getByTestId("login-keycloak-button"));

    expect(window.location.href).toBe("/api/auth/login/keycloak?redirect=%2Flabels");
  });
});
