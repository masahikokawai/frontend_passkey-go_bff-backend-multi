// パスキー登録/ログインの一連の儀式(このアプリ自身へのfetch + navigator.credentials呼び出し)
//
// frontend-rails/with-bffのapp/javascript/passkey_client.jsと同じ設計
// (fetch/navigator.credentialsを引数として受け取れるようにし、DOM操作を一切行わず
// 結果を判別可能なオブジェクトで返すだけにすることで、node --testからブラウザ無しで検証できる)。
// 呼び出し側(login.html.erb/account.html.erb)は、この戻り値を見てalert/status表示/画面遷移だけを担当する。
//
// with-bffとの違い:
// - このアプリはbff-railsのような別オリジンのAPIではなく自分自身を呼ぶため、pathに"/api"も
//   別オリジンのoriginも付かない("/auth/passkey/...")
// - このアプリはActionController::Base(CSRF保護が有効)のため、fetchのヘッダに
//   X-CSRF-Tokenを含める必要がある(bff-railsはActionController::APIでCSRF保護自体が無い)。
//   csrfTokenも他の依存(fetchImpl/credentialsApi)と同じくDIで受け取る
import { base64urlToBuffer, bufferToBase64url } from "./webauthn_codec.js";

// PublicKeyCredentialをこのアプリのbackendへ送るJSON形状に変換する(webauthn gem側が期待する形)
// create()由来(登録、attestationObjectを持つ)/get()由来(ログイン、authenticatorDataを持つ)の両方に対応する
export function credentialToJSON(credential) {
  const json = {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    clientExtensionResults: credential.getClientExtensionResults(),
  };
  if (credential.response.attestationObject) {
    json.response = {
      attestationObject: bufferToBase64url(credential.response.attestationObject),
      clientDataJSON: bufferToBase64url(credential.response.clientDataJSON),
      transports: credential.response.getTransports ? credential.response.getTransports() : [],
    };
  } else {
    json.response = {
      authenticatorData: bufferToBase64url(credential.response.authenticatorData),
      clientDataJSON: bufferToBase64url(credential.response.clientDataJSON),
      signature: bufferToBase64url(credential.response.signature),
      userHandle: credential.response.userHandle ? bufferToBase64url(credential.response.userHandle) : null,
    };
  }
  return json;
}

// パスキーでのログイン(discoverable credential、事前のメールアドレス入力は不要)
// 戻り値: {ok:true} / {ok:false, reason:"begin_failed"|"finish_failed"|"exception", ...}
export async function loginWithPasskey({ fetchImpl, credentialsApi, csrfToken }) {
  try {
    const beginResp = await fetchImpl("/auth/passkey/login/begin", {
      method: "POST",
      credentials: "include",
      headers: { "X-CSRF-Token": csrfToken },
    });
    if (!beginResp.ok) {
      return { ok: false, reason: "begin_failed", status: beginResp.status };
    }
    const beginBody = await beginResp.json();
    const options = beginBody.options;
    options.challenge = base64urlToBuffer(options.challenge);
    (options.allowCredentials || []).forEach((c) => {
      c.id = base64urlToBuffer(c.id);
    });

    const credential = await credentialsApi.get({ publicKey: options });

    const finishResp = await fetchImpl("/auth/passkey/login/finish", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
      body: JSON.stringify({ state: beginBody.state, credential: credentialToJSON(credential) }),
    });
    if (!finishResp.ok) {
      const body = await finishResp.json().catch(() => ({}));
      return { ok: false, reason: "finish_failed", status: finishResp.status, error: body.error };
    }
    return { ok: true };
  } catch (e) {
    return { ok: false, reason: "exception", error: String(e) };
  }
}

// パスキーの新規登録(要: このアプリで既にKeycloakログイン済みのセッションであること)
// 戻り値: {ok:true} / {ok:false, reason:"unauthenticated"|"begin_failed"|"finish_failed"|"exception", ...}
export async function registerPasskeyCeremony({ fetchImpl, credentialsApi, csrfToken }) {
  try {
    const beginResp = await fetchImpl("/auth/passkey/register/begin", {
      method: "POST",
      credentials: "include",
      headers: { "X-CSRF-Token": csrfToken },
    });
    if (beginResp.status === 401) {
      return { ok: false, reason: "unauthenticated" };
    }
    if (!beginResp.ok) {
      return { ok: false, reason: "begin_failed", status: beginResp.status };
    }
    const beginBody = await beginResp.json();
    const options = beginBody.options;
    options.challenge = base64urlToBuffer(options.challenge);
    options.user.id = base64urlToBuffer(options.user.id);

    const credential = await credentialsApi.create({ publicKey: options });

    const finishResp = await fetchImpl("/auth/passkey/register/finish", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
      body: JSON.stringify({ state: beginBody.state, credential: credentialToJSON(credential) }),
    });
    if (!finishResp.ok) {
      const body = await finishResp.json().catch(() => ({}));
      return { ok: false, reason: "finish_failed", status: finishResp.status, error: body.error };
    }
    return { ok: true };
  } catch (e) {
    return { ok: false, reason: "exception", error: String(e) };
  }
}
