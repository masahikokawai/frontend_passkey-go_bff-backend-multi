// BFFへの共通fetchラッパー
//
// BFFパターンの前提により、アクセストークン自体はブラウザに一切渡らない
// (CONTRACT.md セクション2)
// そのためここでは「セッションCookieを自動送信する」
// 「CSRFトークンをヘッダに付与する」「401を検知したらログイン画面へ戻す」という
// BFFとの結合部分だけを担当し、トークンのリフレッシュ等はすべてBFF側が隠蔽する

const STATE_CHANGING_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

function readCookie(name: string): string | null {
  const match = document.cookie.match(new RegExp(`(?:^|; )${name}=([^;]*)`));
  return match ? decodeURIComponent(match[1]) : null;
}

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);
  if (!headers.has("Content-Type") && init.body) {
    headers.set("Content-Type", "application/json");
  }

  if (STATE_CHANGING_METHODS.has(method)) {
    // Double Submit Cookie方式(CONTRACT.md セクション2): BFFが発行した非HttpOnlyの
    // csrf_token Cookieの値を、そのままヘッダに複製して送る
    // BFF 側は Cookie 値とヘッダ値が一致するかだけを検証する(値そのものに秘密の意味はない)
    const csrfToken = readCookie("csrf_token");
    if (csrfToken) {
      headers.set("X-CSRF-Token", csrfToken);
    }
  }

  const response = await fetch(path, {
    ...init,
    method,
    headers,
    // HttpOnlyセッションCookieを送るために同一オリジン扱いでも明示しておく
    credentials: "include",
  });

  if (response.status === 401) {
    // BFFが「ログインしていない/セッション失効」と判断した合図
    // アクセストークンのリフレッシュ可否はBFFが既に判断済みなので、
    // フロントは現在地を保持してログイン導線に戻るだけでよい
    const redirect = encodeURIComponent(window.location.pathname + window.location.search);
    window.location.href = `/login?redirect=${redirect}`;
    throw new ApiError(401, "unauthorized");
  }

  if (!response.ok) {
    // 【テスト監査で発見・修正】backendのエラーレスポンスは常に{"error":"...","message":"..."}の
    // ようなJSONだが、以前はここでbodyをJSON.parseせずtextのまま ApiError.message に入れていた。
    // このmessageは各画面でerr.messageとしてそのままユーザーに表示されるため、
    // 生のJSON文字列(波括弧やキー名込み)がエラーメッセージとして画面に出てしまっていた。
    // 5言語のbackend実装比較という性質上、どこかの言語がJSON以外(HTMLのエラーページ等)を
    // 返す可能性もあるため、JSON.parseに失敗した場合は元のtextへフォールバックする
    const bodyText = await response.text();
    let message = bodyText || response.statusText;
    try {
      const parsed = JSON.parse(bodyText) as { message?: unknown; error?: unknown };
      if (parsed && typeof parsed === "object") {
        const extracted = parsed.message ?? parsed.error;
        if (typeof extracted === "string" && extracted) {
          message = extracted;
        }
      }
    } catch {
      // JSONでなければbodyTextをそのまま使う(何もしない)
    }
    throw new ApiError(response.status, message);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  // 【テスト監査で発見・修正】以前はresponse.json()を直接呼んでおり、200応答でも
  // ボディがJSONとして解析できない場合(プロキシ層の異常等でHTML/空文字が返るケース)、
  // ブラウザ組み込みのSyntaxError("Unexpected token...")がそのままcatch側へ伝播し、
  // 各画面のエラーメッセージが意味不明な文言になっていた。ApiErrorとして
  // 統一的に扱えるよう、ここで一度textとして読んでからJSON.parseする
  const successBodyText = await response.text();
  try {
    return JSON.parse(successBodyText) as T;
  } catch {
    throw new ApiError(response.status, "サーバーからの応答を解析できませんでした");
  }
}
