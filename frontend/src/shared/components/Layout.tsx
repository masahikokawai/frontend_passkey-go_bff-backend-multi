import { useState, type ReactNode } from "react";
import { apiFetch } from "@/shared/api/client";
import type { AuthUser } from "@/shared/hooks/useAuth";

interface LayoutProps {
  user: AuthUser;
  children: ReactNode;
}

interface LogoutResponse {
  redirectUrl: string;
}

export default function Layout({ user, children }: LayoutProps) {
  const [logoutError, setLogoutError] = useState<string | null>(null);

  async function handleLogout() {
    // CONTRACT.md セクション2ではログアウトを「302でKeycloakのend_session_endpointへ」
    // と定義しているが、fetchによるPOSTでは302の外部リダイレクトをブラウザの実際の
    // ナビゲーションとして扱えない(fetchが内部で追従してレスポンスを消費してしまう)
    // そのためBFFの POST /api/auth/logout は 302 ではなく
    // `{ "redirectUrl": "https://keycloak/...(end_session_endpoint)" }` を返す実装とし、
    // フロントが window.location.href で実際に遷移する形に倣う
    // (bff側の実装と齟齬がないか、統合時に要確認)
    //
    // 【delete系と同じパターンで見つかったバグ】以前はここにtry/catchが無く、
    // ログアウトAPIが失敗すると未処理のPromise rejectionになりボタンが無反応に
    // 見えていた(create/update/deleteの各handlerと同じ扱いにする)
    setLogoutError(null);
    try {
      const { redirectUrl } = await apiFetch<LogoutResponse>("/api/auth/logout", {
        method: "POST",
      });
      window.location.href = redirectUrl;
    } catch (err) {
      setLogoutError(err instanceof Error ? err.message : "ログアウトに失敗しました");
    }
  }

  return (
    <div className="container mt-4">
      <nav className="navbar navbar-expand navbar-light bg-light mb-4">
        <div className="container-fluid">
          <a className="navbar-brand" href="/tasks">
            training-go bff-gin
          </a>
          <ul className="navbar-nav me-auto">
            <li className="nav-item">
              <a className="nav-link" href="/tasks">
                タスク
              </a>
            </li>
            <li className="nav-item">
              <a className="nav-link" href="/labels">
                ラベル
              </a>
            </li>
            <li className="nav-item">
              <a className="nav-link" href="/account">
                アカウント設定
              </a>
            </li>
          </ul>
          <div className="d-flex align-items-center">
            <span className="me-3">
              {user.name}({user.role})
            </span>
            <button
              type="button"
              className="btn btn-outline-secondary btn-sm"
              onClick={handleLogout}
              data-testid="logout-button"
            >
              ログアウト
            </button>
          </div>
        </div>
      </nav>
      {logoutError && <div className="alert alert-danger">{logoutError}</div>}
      {children}
    </div>
  );
}
