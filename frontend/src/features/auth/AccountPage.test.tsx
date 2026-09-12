import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import AccountPage from "./AccountPage";
import { PasskeyUnsupportedError } from "@/shared/webauthn/passkey";

// (LoginForm.test.tsxと同じで)navigator.credentials自体はjsdomに実装が無いので shared/webauthn/passkey.tsをモックし、
// AccountPageからの呼び出し方(成功/失敗時の表示切り替え・端末名の受け渡し)だけを検証する
//
// このテストが追加されるまで AccountPage.tsx は Vitest/Jest どちらもテストが1件も無かった
const { registerPasskeyMock } = vi.hoisted(() => ({ registerPasskeyMock: vi.fn() }));
vi.mock("@/shared/webauthn/passkey", async () => {
  const actual = await vi.importActual<typeof import("@/shared/webauthn/passkey")>(
    "@/shared/webauthn/passkey"
  );
  return { ...actual, registerPasskey: registerPasskeyMock };
});

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
    expect(screen.queryByTestId("passkey-register-error")).not.toBeInTheDocument();
  });

  it("端末名を入力しなかった場合、undefinedを渡す(空文字ではなく)", async () => {
    // AccountPage.tsxの実装は `deviceName || undefined` なので、空文字がそのまま
    // backendへ渡ってしまう(空文字のname列になる)事故を防ぐ意図を確認する
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

  it("その他のエラー(backendの422等)の場合、ローカル認証ユーザー専用である旨の汎用メッセージを表示する", async () => {
    // CONTRACT.mdセクション22.1: パスキーはbffのローカル認証(HMAC/RSA)ユーザー専用
    // Keycloakログイン中のユーザーが登録しようとした場合もこの分岐に落ちる
    registerPasskeyMock.mockRejectedValue(new Error("422 scope error"));
    render(<AccountPage />);

    await userEvent.click(screen.getByTestId("passkey-register-button"));

    await waitFor(() => {
      expect(screen.getByTestId("passkey-register-error")).toHaveTextContent(
        "ローカル認証(HMAC/RSA)でログインしているユーザーのみ登録できます"
      );
    });
    expect(screen.queryByTestId("passkey-register-success")).not.toBeInTheDocument();
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

  // 【2回目のテスト監査で追加】LoginForm.test.tsxの同名テストと同じ理由
  // (navigator.credentials.create()はユーザー操作待ちで長時間ブロックしうるため、
  // 待機中に別画面へ遷移してAccountPageがアンマウントされる余地が実際にある)
  it("登録待ちの間にアンマウントされても、Reactの状態更新警告が出ない", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
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
