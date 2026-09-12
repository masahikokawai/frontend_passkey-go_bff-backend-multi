// パスキー登録/ログインの一連の儀式(bff-railsへのfetch + navigator.credentials呼び出し)
//
// 【2回目のテスト監査で切り出し】
// この一連のロジックは元々app/views/home/index.html.erb の
// <script> に DOM操作(alert/status表示/location.reload)と混ざったまま書かれており、
// webauthn_codec.js 切り出し後もテストが1件も無かった
//
// fetch/navigator.credentialsを
// 引数として受け取れるようにし(依存性注入)、DOM操作を一切行わず結果を判別可能な
// オブジェクトで返すだけにすることで、node --testからブラウザ無しで検証できるようにした
//
// 呼び出し側(index.html.erb)は、この戻り値を見てalert/status文言/画面遷移だけを担当する
// (「何が起きたか」の判定ロジックと「どう表示するか」を分離する、通常のUIコンポーネント設計と同じ考え方)
//
// 【2回目のテスト監査で判明】
// "webauthn_codec"というbare specifier(importmap-rails の pin名)はブラウザ側では解決できるが、
// 素のNode(node --test)は importmap を理解しないため ERR_MODULE_NOT_FOUND になる
// 同じディレクトリ内の相対importに変えることで、ブラウザ側
// (importmap-rails が配信する app/javascript/ を素直に相対解決できる)
// と Node 側の両方で解決できるようにした
import { base64urlToBuffer, bufferToBase64url } from "./webauthn_codec.js";

// PublicKeyCredentialをbff-railsへ送るJSON形状に変換する(webauthn gem側が期待する形)
// create()由来(登録、attestationObjectを持つ)/get()由来(ログイン、authenticatorDataを持つ)
// の両方に対応する
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
export async function loginWithPasskey({ origin, fetchImpl, credentialsApi }) {
  try {
    const beginResp = await fetchImpl(origin + "/api/auth/passkey/login/begin", {
      method: "POST",
      credentials: "include",
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

    const finishResp = await fetchImpl(origin + "/api/auth/passkey/login/finish", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
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

// パスキーの新規登録(要: bff-rails側で既にログイン済みのセッションであること)
// 戻り値: {ok:true} / {ok:false, reason:"unauthenticated"|"begin_failed"|"finish_failed"|"exception", ...}
export async function registerPasskeyCeremony({ origin, fetchImpl, credentialsApi }) {
  try {
    const beginResp = await fetchImpl(origin + "/api/auth/passkey/register/begin", {
      method: "POST",
      credentials: "include",
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

    const finishResp = await fetchImpl(origin + "/api/auth/passkey/register/finish", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
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
