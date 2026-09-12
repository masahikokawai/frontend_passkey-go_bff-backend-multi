// パスキー(WebAuthn)関連のブラウザAPI呼び出し(CONTRACT.mdセクション22.2/22.6)
//
// 追加ライブラリは使わず、ブラウザ標準の navigator.credentials.create()/.get() を直接呼ぶ
// base64url⇔ArrayBufferの変換も、モダンブラウザ(Chrome 126+/Safari 18+/
// Firefox 141+)が標準搭載している PublicKeyCredential.parseCreationOptionsFromJSON /
// parseRequestOptionsFromJSON / credential.toJSON() に任せることで自前実装を避けている
//
// クロスデバイス認証(QRコード表示→スマホでスキャン→スマホでの生体認証→PC側ブラウザへ
// 結果が戻る)はブラウザ/OSの標準機能(hybrid transport)であり、ここから特別に呼び出す必要はない
// navigator.credentials.get()を正しく呼べば、ブラウザが自動的にこの導線を提示する

import { apiFetch } from "@/shared/api/client";

export class PasskeyUnsupportedError extends Error {
  constructor() {
    super("このブラウザはパスキー(WebAuthn)に対応していません");
  }
}

function assertSupported() {
  if (
    typeof window === "undefined" ||
    !window.PublicKeyCredential ||
    typeof PublicKeyCredential.parseCreationOptionsFromJSON !== "function" ||
    typeof PublicKeyCredential.parseRequestOptionsFromJSON !== "function"
  ) {
    throw new PasskeyUnsupportedError();
  }
}

interface RegisterBeginResponse {
  publicKey: PublicKeyCredentialCreationOptionsJSON;
}

// register/finishのレスポンス形状は {"registered": true} だけなので型は付けない

/**
 * ログイン中ユーザーの新しいパスキーを登録する(要ログイン、CONTRACT.mdセクション22.5)
 * apiFetchを使うため、セッションCookie・CSRFトークンは自動で付与される
 */
export async function registerPasskey(deviceName?: string): Promise<void> {
  assertSupported();

  const begin = await apiFetch<RegisterBeginResponse>("/api/auth/passkey/register/begin", {
    method: "POST",
  });

  const options = PublicKeyCredential.parseCreationOptionsFromJSON(begin.publicKey);
  const credential = (await navigator.credentials.create({ publicKey: options })) as PublicKeyCredential | null;
  if (!credential) {
    throw new Error("パスキーの作成がキャンセルされました");
  }

  const query = deviceName ? `?name=${encodeURIComponent(deviceName)}` : "";
  await apiFetch(`/api/auth/passkey/register/finish${query}`, {
    method: "POST",
    body: JSON.stringify(credential.toJSON()),
  });
}

interface LoginBeginResponse {
  publicKey: PublicKeyCredentialRequestOptionsJSON;
  state: string;
}

/**
 * discoverable credential(resident key)方式でのパスキーログイン(未ログイン状態から呼べる)
 * 成功時はbff側でセッションCookieが発行されるため、呼び出し元は画面遷移するだけでよい
 */
export async function loginWithPasskey(): Promise<void> {
  assertSupported();

  // 素のfetchを使う理由はLoginForm.tsxのローカルログインと同じ:
  // apiFetchは401を検知すると即座に/loginへリダイレクトする実装のため、
  // ログイン前の呼び出しには使えない(まだセッションが無い状態が401扱いされてしまう)
  const beginResponse = await fetch("/api/auth/passkey/login/begin", {
    method: "POST",
    credentials: "include",
  });
  if (!beginResponse.ok) {
    throw new Error("パスキーログインの開始に失敗しました");
  }
  const begin = (await beginResponse.json()) as LoginBeginResponse;

  const options = PublicKeyCredential.parseRequestOptionsFromJSON(begin.publicKey);
  const credential = (await navigator.credentials.get({ publicKey: options })) as PublicKeyCredential | null;
  if (!credential) {
    throw new Error("パスキーでのログインがキャンセルされました");
  }

  const finishResponse = await fetch(
    `/api/auth/passkey/login/finish?state=${encodeURIComponent(begin.state)}`,
    {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(credential.toJSON()),
    }
  );
  if (!finishResponse.ok) {
    throw new Error("パスキーの検証に失敗しました");
  }
}
