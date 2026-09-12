import { createBrowserRouter, Navigate, Outlet, RouterProvider } from "react-router-dom";
import Layout from "@/shared/components/Layout";
import { useAuth } from "@/shared/hooks/useAuth";
import type { FeatureFlagContext } from "@/shared/hooks/useFeatureFlag";
import TaskListSwitch from "@/features/tasks/TaskListSwitch";
import TaskFormPageSwitch from "@/features/tasks/TaskFormPageSwitch";
import LabelList from "@/features/labels/LabelList";
import LoginForm from "@/features/auth/LoginForm";
import AccountPage from "@/features/auth/AccountPage";

// ログイン必須ルートの共通ガード
// Rails: ApplicationController#require_session に相当(CONTRACT.md セクション6)
function ProtectedLayout() {
  const auth = useAuth();

  if (auth.status === "loading") {
    return <div className="container mt-4">読み込み中...</div>;
  }
  if (auth.status === "unauthenticated") {
    // apiFetch側で既に /login へのリダイレクトが発生しているはずなので、
    // ここでは遷移完了までの一瞬何も描画しない
    return null;
  }

  const context: FeatureFlagContext = { featureFlags: auth.me.feature_flags };

  return (
    <Layout user={auth.me.user}>
      <Outlet context={context} />
    </Layout>
  );
}

const routeConfig = [
  {
    path: "/login",
    element: <LoginForm loginPath="/api/auth/login" variantLabel="HMAC版・既定" />,
  },
  {
    path: "/login/rsa",
    element: <LoginForm loginPath="/api/auth/login/rsa" variantLabel="RSA版" />,
  },
  {
    path: "/",
    element: <ProtectedLayout />,
    children: [
      { index: true, element: <Navigate to="/tasks" replace /> },
      { path: "tasks", element: <TaskListSwitch /> },
      // CONTRACT.mdセクション19: frontend.task-create-ux="page"のときにTaskListPageの
      // 「タスクを登録」リンク・各行の「編集」リンクから遷移する専用ルート
      // CONTRACT.mdセクション19.7: TS/JSどちらの実装から遷移してきても正しい方の
      // フォームページを表示できるよう、TaskFormPageSwitchでfrontend.tasks-ts-rewriteを見て振り分ける
      { path: "tasks/new", element: <TaskFormPageSwitch mode="create" /> },
      { path: "tasks/:id/edit", element: <TaskFormPageSwitch mode="edit" /> },
      { path: "labels", element: <LabelList /> },
      // CONTRACT.mdセクション22.6: パスキー登録用のアカウント設定画面
      { path: "account", element: <AccountPage /> },
    ],
  },
];

// createAppRouter はrouteConfigから新しいrouterインスタンスを作るファクトリ
//
// 【3回目のテスト監査で追加、根本原因の修正】以前はrouterをこのモジュールの
// トップレベルで1回だけ生成するシングルトンとしてexportしており、Jest側のテスト
// (test-jest/routes.test.tsx)はテストごとに新しいnavigate状態がほしいという理由で
// `await import("./routes")` の動的import+`jest.resetModules()`(またはisolateModules)を
// 使って「モジュールを再読み込みすることでrouterを作り直す」という手法を取っていた
//
// ところがJestのモジュールレジストリのリセット/隔離は、react-routerが内部で
// requireしているreact自体まで再読み込みの対象にしてしまい、react-dom側が
// 握っている(最初に読み込まれた)reactインスタンスと食い違って
// "Invalid hook call"(dispatcherがnull)になっていた
// (Vitestではこの問題が起きないため、パターン自体は誤りではなかった)
//
// この関数を素直にexportし、テスト側は「モジュールを再読み込みする」のではなく
// 「この関数を呼んで新しいrouterインスタンスを作る」だけで済むようにすることで、
// モジュールレジストリに一切触れずにテストごとの状態分離を実現できる
// (react/react-dom/react-routerは通常通り1回だけ読み込まれたままになる)
export function createAppRouter() {
  return createBrowserRouter(routeConfig);
}

export const router = createAppRouter();

export function AppRouter({ router: routerProp }: { router?: ReturnType<typeof createAppRouter> } = {}) {
  return <RouterProvider router={routerProp ?? router} />;
}
