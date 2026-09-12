import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type { AuthState } from "@/shared/hooks/useAuth";
import { AppRouter, createAppRouter } from "./routes";

// useAuthをモックし、ProtectedLayoutの3状態(loading/unauthenticated/authenticated)
// それぞれで実際にどの画面が表示されるかをルーティングごと検証する
// (個々のコンポーネント自体の単体テストは別ファイルにあるため、ここではルーター配線
// (/login, /login/rsa, 保護ルート)が正しくコンポーネントへ振り分けられているかだけを見る)
let mockAuthState: AuthState = { status: "loading" };
vi.mock("@/shared/hooks/useAuth", () => ({
  useAuth: () => mockAuthState,
}));

// TaskListSwitch/LabelListは実体が重い(API呼び出し・feature flag評価)ため、
// ルーティングの検証に必要ない範囲はダミーに差し替える
vi.mock("@/features/tasks/TaskListSwitch", () => ({
  default: () => <div data-testid="task-list-switch-stub" />,
}));
vi.mock("@/features/labels/LabelList", () => ({
  default: () => <div data-testid="label-list-stub" />,
}));

// 【3回目のテスト監査(Jest版で発見)で追記】
// 以前はここも vi.resetModules() + 動的importで「テストごとに新しいrouterインスタンスを作る」手法を取っていた
// Vitestではこのパターン自体は問題無く動くが(Jest版と違いreactの二重読み込みは
// 起きない)、routes.tsxにcreateAppRouter()ファクトリを追加したことで、
// モジュールの再読み込みという回りくどい手段を使わずに済むようになったため、
// Jest版と同じ書き方に揃えた
//
// Jest版はモジュール再読み込みが実際にバグを引き起こしていたため、この書き方への変更が必須だった
// 詳細は test-jest/routes.test.tsx のコメント参照
async function renderAt(path: string) {
  const router = createAppRouter();
  await router.navigate(path);
  return render(<AppRouter router={router} />);
}

describe("router", () => {
  it("/login はLoginForm(HMAC版)を表示する", async () => {
    mockAuthState = { status: "unauthenticated" };
    await renderAt("/login");
    expect(await screen.findByRole("heading", { name: /ログイン.*HMAC版/ })).toBeInTheDocument();
  });

  it("/login/rsa はLoginForm(RSA版)を表示する", async () => {
    mockAuthState = { status: "unauthenticated" };
    await renderAt("/login/rsa");
    expect(await screen.findByRole("heading", { name: /ログイン.*RSA版/ })).toBeInTheDocument();
  });

  it("認証確認中(loading)は保護ルートで読み込み中の表示になる", async () => {
    mockAuthState = { status: "loading" };
    await renderAt("/tasks");
    expect(screen.getByText("読み込み中...")).toBeInTheDocument();
  });

  it("未認証(unauthenticated)は保護ルートで何も表示しない(apiFetch側のリダイレクト待ち)", async () => {
    mockAuthState = { status: "unauthenticated" };
    const { container } = await renderAt("/tasks");
    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it("認証済み(authenticated)は/tasksでLayout配下にTaskListSwitchを表示する", async () => {
    mockAuthState = {
      status: "authenticated",
      me: {
        user: { id: 1, name: "太郎", email: "a@example.com", role: "general" },
        feature_flags: {},
      },
    };
    await renderAt("/tasks");
    expect(await screen.findByTestId("task-list-switch-stub")).toBeInTheDocument();
    expect(screen.getByText(/太郎/)).toBeInTheDocument();
  });

  it("認証済みで/labelsへ遷移するとLabelListを表示する", async () => {
    mockAuthState = {
      status: "authenticated",
      me: {
        user: { id: 1, name: "太郎", email: "a@example.com", role: "general" },
        feature_flags: {},
      },
    };
    await renderAt("/labels");
    expect(await screen.findByTestId("label-list-stub")).toBeInTheDocument();
  });

  it("認証済みで/accountへ遷移するとAccountPage(パスキー登録画面)を表示する", async () => {
    // 【統合レビューで判明】/accountルート(routes.tsx)自体はCONTRACT.mdセクション22.6で
    // 追加されていたが、ルーティングレベルのテストが1件も無かった(AccountPage単体の
    // 表示ロジックはAccountPage.test.tsxでカバー済みだが、「保護ルートとして正しく
    // 登録されているか」は別の関心事のためここで確認する)
    mockAuthState = {
      status: "authenticated",
      me: {
        user: { id: 1, name: "太郎", email: "a@example.com", role: "general" },
        feature_flags: {},
      },
    };
    await renderAt("/account");
    expect(await screen.findByRole("heading", { name: "アカウント設定" })).toBeInTheDocument();
    expect(screen.getByTestId("passkey-register-button")).toBeInTheDocument();
  });

  it("未認証で/accountへ遷移すると何も表示しない(他の保護ルートと同じくapiFetch側のリダイレクト待ち)", async () => {
    mockAuthState = { status: "unauthenticated" };
    const { container } = await renderAt("/account");
    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it("認証済みで/へ遷移すると/tasksへリダイレクトされる", async () => {
    mockAuthState = {
      status: "authenticated",
      me: {
        user: { id: 1, name: "太郎", email: "a@example.com", role: "general" },
        feature_flags: {},
      },
    };
    await renderAt("/");
    expect(await screen.findByTestId("task-list-switch-stub")).toBeInTheDocument();
  });
});
