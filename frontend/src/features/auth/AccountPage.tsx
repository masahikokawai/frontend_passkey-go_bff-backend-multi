import { useEffect, useRef, useState } from "react";
import { registerPasskey, PasskeyUnsupportedError } from "@/shared/webauthn/passkey";

// CONTRACT.mdセクション22.6: 設定/アカウント画面(新設)
// 要ログイン(ProtectedLayout配下)
// パスキーはbffのローカル認証(HMAC/RSA)ユーザー専用の追加認証手段のため、
// Keycloakログイン中のユーザーが押すと422相当のエラーになる(bff側でスコープ外として拒否)
export default function AccountPage() {
  const [deviceName, setDeviceName] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  // 【2回目のテスト監査で追記】navigator.credentials.create()はユーザーの生体認証待ちで
  // 数秒〜数十秒ブロックしうるため、その間にユーザーが別画面へ遷移してこのコンポーネントが
  // アンマウントされる余地が実際にある(useAuth.tsの`cancelled`フラグと同じ理由の対策)
  //
  // ガードが無いと、アンマウント後に setState 系を呼び出し React の警告が発生する
  // "Can't perform a state update on an unmounted component"
  const mountedRef = useRef(true);
  useEffect(() => {
    return () => {
      mountedRef.current = false;
    };
  }, []);

  async function handleRegister() {
    setSubmitting(true);
    setMessage(null);
    setError(null);
    try {
      await registerPasskey(deviceName || undefined);
      if (!mountedRef.current) return;
      setMessage("パスキーを登録しました");
      setDeviceName("");
    } catch (err) {
      if (!mountedRef.current) return;
      setError(
        err instanceof PasskeyUnsupportedError
          ? err.message
          : "パスキーの登録に失敗しました(ローカル認証(HMAC/RSA)でログインしているユーザーのみ登録できます)"
      );
    } finally {
      if (mountedRef.current) setSubmitting(false);
    }
  }

  return (
    <div className="container mt-4" style={{ maxWidth: "480px" }}>
      <h1 className="h4 mb-3">アカウント設定</h1>

      <section>
        <h2 className="h6">パスキーの登録</h2>
        <p className="text-muted">
          次回以降、メールアドレス・パスワードの入力無しでログインできるようになります。
        </p>

        {message && (
          <div className="alert alert-success" role="alert" data-testid="passkey-register-success">
            {message}
          </div>
        )}
        {error && (
          <div className="alert alert-danger" role="alert" data-testid="passkey-register-error">
            {error}
          </div>
        )}

        <div className="form-group">
          <label htmlFor="passkey-device-name">端末名(任意)</label>
          <input
            id="passkey-device-name"
            type="text"
            className="form-control"
            value={deviceName}
            onChange={(e) => setDeviceName(e.target.value)}
            data-testid="passkey-device-name-input"
          />
        </div>

        <button
          type="button"
          className="btn btn-primary"
          onClick={handleRegister}
          disabled={submitting}
          data-testid="passkey-register-button"
        >
          パスキーを登録
        </button>
      </section>
    </div>
  );
}
