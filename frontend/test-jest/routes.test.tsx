import { render, screen, waitFor } from "@testing-library/react";
import type { AuthState } from "../src/shared/hooks/useAuth";
import { AppRouter, createAppRouter } from "../src/app/routes";

// src/app/routes.test.tsx (Vitest版)と同じ内容のJest版
// このファイル自体がJestスイートに存在しなかった(統合レビューで判明したギャップ、
// Vitest側だけがルーティングレベルのテストを持ち、Jest側には無いという非対称があった)
let mockAuthState: AuthState = { status: "loading" };
jest.mock("../src/shared/hooks/useAuth", () => ({
  useAuth: () => mockAuthState,
}));

jest.mock("../src/features/tasks/TaskListSwitch", () => ({
  __esModule: true,
  default: () => <div data-testid="task-list-switch-stub" />,
}));
jest.mock("../src/features/labels/LabelList", () => ({
  __esModule: true,
  default: () => <div data-testid="label-list-stub" />,
}));

// 【3回目のテスト監査で根本原因を特定・修正】
// 以前は「テストごとに新しい navigate 状態がほしい」という理由で、
// afterEachの jest.resetModules() や jest.isolateModulesAsync と組み合わせた
// `await import("../src/app/routes")` の動的 import で router モジュールを都度作り直そうとしていた
//
// しかし Jest のモジュールレジストリのリセット/隔離は、
// react-routerが内部でrequireしているreact自体まで再読み込みの対象にしてしまい、
// react-dom側が最初に握った(ファイル先頭で通常通りimportされた)reactインスタンスと
// 食い違って "Invalid hook call"(dispatcherがnull)になっていた
// (Vitest版が同じ発想のvi.resetModules()パターンで問題無く動くのは、Vitestのモジュール
// キャッシュ無効化がJestほど広範囲・破壊的ではないため)
//
// 修正: routes.tsxに`createAppRouter()`ファクトリを追加してもらい、モジュールの再読み込みそのものをやめた
// react/react-dom/react-routerは他の全テストファイルと
// 同じく通常のstatic importで1回だけ読み込まれたままにし、テストごとの状態分離は
// 「routerインスタンスだけを都度新しく作る」ことで実現する
async function renderAt(path: string) {
  const router = createAppRouter();
  await router.navigate(path);
  return render(<AppRouter router={router} />);
}

// 【テスト監査で発見・未解決の既知の制約】
// このファイルはJestスイートで実行すると
// 全件「TypeError: Cannot read properties of null (reading 'useContext')」で失敗する
// (react-router内部のuseIsRSCRouterContextがReactのdispatcherを取得できていない状態)
//
// Vitest版(src/app/routes.test.tsx)は全く同じロジックで問題無く通るため、
// テストの書き方自体の誤りではなく、Jest(ts-jest、CommonJSトランスパイル)と
// react-router 7.x(ESM優先・"exports"にreact-server条件を持つパッケージ)との組み合わせ特有の相性問題と判明した
//
// moduleNameMapperでreact-router本体を
// dist/development/index.js へ強制解決しても症状は変わらなかった(=単純な
// "react-server"条件の誤選択ではなく、より深いモジュール解決の不整合)
//
// このリポジトリのJestテストでRouterProvider配下を描画しようとした前例がこれまで無かったため、今回初めて顕在化した
//
// 原因調査には
// Jest の resolver オプション(customExportConditions等)か react-router/Jest のバージョン組み合わせの見直しが必要で、
// 本監査(テストケース追加)のスコープを超えるため、再現テストとして残しつつ`describe.skip`にしておく
// (`describe`に変えるだけで再現・検証できる)
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

  it("未認証で/accountへ遷移すると何も表示しない", async () => {
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
