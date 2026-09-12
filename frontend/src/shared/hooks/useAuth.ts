import { useEffect, useState } from "react";
import { apiFetch, ApiError } from "@/shared/api/client";

export interface AuthUser {
  id: number;
  name: string;
  email: string;
  role: "general" | "management";
}

// CONTRACT.md セクション3: GET /api/me のレスポンス形状
export interface MeResponse {
  user: AuthUser;
  feature_flags: Record<string, boolean>;
}

export type AuthState =
  | { status: "loading" }
  | { status: "authenticated"; me: MeResponse }
  | { status: "unauthenticated" };

// ログイン状態確認(CONTRACT.md セクション6): アプリ起動時に /api/me を呼ぶ
// 401の場合、apiFetch が /login へリダイレクトするため、ここでは
// 「リダイレクトが完了するまでの一瞬の状態」として unauthenticated を返すだけでよい
export function useAuth(): AuthState {
  const [state, setState] = useState<AuthState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;

    apiFetch<MeResponse>("/api/me")
      .then((me) => {
        if (!cancelled) {
          setState({ status: "authenticated", me });
        }
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          setState({ status: "unauthenticated" });
          return;
        }
        // ネットワークエラー等
        // 未ログイン扱いにしてログイン導線を出す
        setState({ status: "unauthenticated" });
      });

    return () => {
      cancelled = true;
    };
  }, []);

  return state;
}
