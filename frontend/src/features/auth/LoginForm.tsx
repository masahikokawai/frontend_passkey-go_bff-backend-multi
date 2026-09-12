import { useEffect, useRef, useState, type FormEvent } from "react";
import { loginWithPasskey, PasskeyUnsupportedError } from "@/shared/webauthn/passkey";
import { safeRedirectPath } from "@/shared/url/safeRedirect";

interface LoginFormProps {
  // 送信先のbffエンドポイント
  // CONTRACT.md セクション16.1:
  // "/login" は POST /api/auth/login(HMAC)、"/login/rsa" は POST /api/auth/login/rsa(RSA)
  loginPath: string;
  // 画面見出し・案内文の出し分け(HMAC版/RSA版の違いを学習用に明示するため)
  variantLabel: string;
}

interface LoginSuccessResponse {
  redirect: string;
}

interface LoginErrorResponse {
  error: "invalid_credentials" | "password_expired" | string;
}

const ERROR_MESSAGES: Record<string, string> = {
  invalid_credentials: "メールアドレスまたはパスワードが正しくありません",
  password_expired: "パスワードの有効期限が切れています",
};

// ローカル(非Keycloak)ログインの共通フォーム
// CONTRACT.md セクション16.4/16.6
//
// 【apiFetchを使わない理由】frontend/src/shared/api/client.ts の apiFetch は401を受け取ると即座に `/login` へリダイレクトする実装になっている
// (セッション切れ時の共通処理として)
//
// ログインフォーム自体の送信失敗
// (invalid_credentials/password_expired)も401で返ってくるため、apiFetchを使うとエラー本文を読む前にリダイレクトされてしまい無限ループになる
// そのためログイン前の送信は素のfetchを直接使う(CSRFトークンもまだ発行されていないため不要)
export default function LoginForm({ loginPath, variantLabel }: LoginFormProps) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 【3回目のテスト監査で発見・修正、重要】以前はここでURLSearchParamsの値を
  // そのまま使っており、`?redirect=http://evil.com` のような値を渡されると
  // (特にhandlePasskeyLoginの成功後は)bffを一切経由せずフロントエンド単体で
  // 外部ドメインへリダイレクトしてしまうOpen Redirect脆弱性があった
  // (frontend/src/shared/url/safeRedirect.ts参照)
  const redirectTarget = safeRedirectPath(
    new URLSearchParams(window.location.search).get("redirect")
  );

  // 【2回目のテスト監査で追記】パスキーログイン(下記handlePasskeyLogin)は
  // navigator.credentials.get()のユーザー操作待ちで長時間ブロックしうるため、
  // その間にユーザーが別画面へ遷移してこのコンポーネントがアンマウントされる余地がある
  //
  // 通常のパスワード送信(handleSubmit)は短時間で終わるため実際に踏む可能性は低いが、
  // 同じフォーム内の2つのハンドラで挙動を揃えるため両方にガードを入れる
  // (useAuth.tsの`cancelled`フラグと同じ対策)
  const mountedRef = useRef(true);
  useEffect(() => {
    return () => {
      mountedRef.current = false;
    };
  }, []);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const response = await fetch(loginPath, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ email, password }),
      });

      if (!response.ok) {
        const body = (await response.json().catch(() => ({}))) as Partial<LoginErrorResponse>;
        if (!mountedRef.current) return;
        setError(
          (body.error && ERROR_MESSAGES[body.error]) ?? "ログインに失敗しました"
        );
        setSubmitting(false);
        return;
      }

      const body = (await response.json()) as LoginSuccessResponse;
      window.location.href = body.redirect || redirectTarget;
    } catch {
      if (!mountedRef.current) return;
      setError("ログインに失敗しました");
      setSubmitting(false);
    }
  }

  function handleKeycloakLogin() {
    window.location.href = `/api/auth/login/keycloak?redirect=${encodeURIComponent(redirectTarget)}`;
  }

  // CONTRACT.mdセクション22: bffのローカル認証(HMAC/RSA)ユーザーへの追加の認証手段
  // discoverable credential方式のため、メールアドレスの入力は不要
  async function handlePasskeyLogin() {
    setSubmitting(true);
    setError(null);
    try {
      await loginWithPasskey();
      window.location.href = redirectTarget;
    } catch (err) {
      if (!mountedRef.current) return;
      setError(
        err instanceof PasskeyUnsupportedError
          ? err.message
          : "パスキーでのログインに失敗しました"
      );
      setSubmitting(false);
    }
  }

  return (
    <div className="container mt-4" style={{ maxWidth: "480px" }}>
      <h1 className="h4 mb-3">ログイン({variantLabel})</h1>

      {error && (
        <div className="alert alert-danger" role="alert" data-testid="login-error">
          {error}
        </div>
      )}

      <form onSubmit={handleSubmit}>
        <div className="form-group">
          <label htmlFor="login-email">メールアドレス</label>
          <input
            id="login-email"
            type="email"
            className="form-control"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            data-testid="login-email-input"
          />
        </div>
        <div className="form-group">
          <label htmlFor="login-password">パスワード</label>
          <input
            id="login-password"
            type="password"
            className="form-control"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            data-testid="login-password-input"
          />
        </div>
        <button
          type="submit"
          className="btn btn-primary btn-block"
          disabled={submitting}
          data-testid="login-submit-button"
        >
          ログイン
        </button>
      </form>

      <hr />

      <p>
        {loginPath === "/api/auth/login" ? (
          <a href="/login/rsa" data-testid="login-rsa-link">
            RSA版で試す
          </a>
        ) : (
          <a href="/login" data-testid="login-hmac-link">
            HMAC版(既定)で試す
          </a>
        )}
      </p>

      <button
        type="button"
        className="btn btn-outline-secondary btn-block"
        onClick={handleKeycloakLogin}
        data-testid="login-keycloak-button"
      >
        Keycloakでログイン
      </button>

      <button
        type="button"
        className="btn btn-outline-secondary btn-block mt-2"
        onClick={handlePasskeyLogin}
        disabled={submitting}
        data-testid="login-passkey-button"
      >
        パスキーでログイン
      </button>
    </div>
  );
}
