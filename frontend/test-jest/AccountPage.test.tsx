import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import AccountPage from "../src/features/auth/AccountPage";
import { PasskeyUnsupportedError } from "../src/shared/webauthn/passkey";

// src/features/auth/AccountPage.test.tsx (Vitest版)と同じ内容のJest版
// AccountPage.tsx自体はVitest/Jestどちらのスイートにもテストが1件も無かった
// (統合レビューで判明したギャップ)
const registerPasskeyMock = jest.fn();
jest.mock("../src/shared/webauthn/passkey", () => ({
  ...jest.requireActual("../src/shared/webauthn/passkey"),
  registerPasskey: (...args: unknown[]) => registerPasskeyMock(...args),
}));

afterEach(() => {
  registerPasskeyMock.mockReset();
});

describe("AccountPage", () => {
  it("登録成功時、成功メッセージを表示し、端末名の入力欄をクリアする", async () => {
    registerPasskeyMock.mockResolvedValue(undefined);
    render(<AccountPage />);

    await userEvent.type(screen.getByTestId("passkey-device-name-input"), "MacBook Pro");
    await userEvent.click(screen.getByTestId("passkey-register-button"));

    await waitFor(() => {
      expect(screen.getByTestId("passkey-register-success")).toBeInTheDocument();
    });
    expect(registerPasskeyMock).toHaveBeenCalledWith("MacBook Pro");
    expect(screen.getByTestId("passkey-device-name-input")).toHaveValue("");
  });

  it("端末名を入力しなかった場合、undefinedを渡す(空文字ではなく)", async () => {
    registerPasskeyMock.mockResolvedValue(undefined);
    render(<AccountPage />);

    await userEvent.click(screen.getByTestId("passkey-register-button"));

    await waitFor(() => expect(registerPasskeyMock).toHaveBeenCalledWith(undefined));
  });

  it("WebAuthn未対応ブラウザの場合、PasskeyUnsupportedErrorのmessageをそのまま表示する", async () => {
    registerPasskeyMock.mockRejectedValue(new PasskeyUnsupportedError());
    render(<AccountPage />);

    await userEvent.click(screen.getByTestId("passkey-register-button"));

    await waitFor(() => {
      expect(screen.getByTestId("passkey-register-error")).toHaveTextContent(
        "このブラウザはパスキー(WebAuthn)に対応していません"
      );
    });
  });

  it("その他のエラーの場合、ローカル認証ユーザー専用である旨の汎用メッセージを表示する", async () => {
    registerPasskeyMock.mockRejectedValue(new Error("422 scope error"));
    render(<AccountPage />);

    await userEvent.click(screen.getByTestId("passkey-register-button"));

    await waitFor(() => {
      expect(screen.getByTestId("passkey-register-error")).toHaveTextContent(
        "ローカル認証(HMAC/RSA)でログインしているユーザーのみ登録できます"
      );
    });
  });

  it("送信中はボタンを無効化する", async () => {
    let resolveRegister: () => void = () => {};
    registerPasskeyMock.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveRegister = resolve;
      })
    );
    render(<AccountPage />);

    const button = screen.getByTestId("passkey-register-button");
    await userEvent.click(button);
    expect(button).toBeDisabled();

    resolveRegister();
    await waitFor(() => expect(button).not.toBeDisabled());
  });

  // src/features/auth/AccountPage.test.tsx (Vitest版)と同じ内容のJest版
  it("登録待ちの間にアンマウントされても、Reactの状態更新警告が出ない", async () => {
    const consoleError = jest.spyOn(console, "error").mockImplementation(() => {});
    let rejectRegister: (err: Error) => void = () => {};
    registerPasskeyMock.mockReturnValue(
      new Promise<void>((_resolve, reject) => {
        rejectRegister = reject;
      })
    );

    const { unmount } = render(<AccountPage />);
    await userEvent.click(screen.getByTestId("passkey-register-button"));
    unmount();

    rejectRegister(new Error("too late"));
    await new Promise((r) => setTimeout(r, 0));

    const gotUnmountWarning = consoleError.mock.calls.some((call) =>
      String(call[0]).includes("state update on an unmounted component")
    );
    expect(gotUnmountWarning).toBe(false);
    consoleError.mockRestore();
  });
});
