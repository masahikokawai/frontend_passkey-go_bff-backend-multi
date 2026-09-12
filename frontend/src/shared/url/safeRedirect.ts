// Open Redirect対策(CWE-601)
//
// 【3回目のテスト監査で発見】ログイン画面の `?redirect=<path>` クエリパラメータは、
// ログイン成功後に window.location.href へそのまま代入される箇所が複数ある
// (LoginForm.tsxのパスキーログイン成功時等)
// ここでpathが `http://evil.com` や `//evil.com`(protocol-relative URL)のような値だと、bffを一切経由せず
// フロントエンド単体で外部ドメインへ誘導されてしまう
//
// 特にパスキーログインは
// 「本人確認(生体認証等)は済んでいる」という安心感があるため、直後に別サイトへ
// 飛ばされても違和感を持たれにくく、フィッシングに悪用されやすい
//
// bff側(LoginKeycloak/Callback)にも同種の検証を追加済みだが、
// - パスキーログインの成功後リダイレクトはbffを経由しない(フロントエンド単体の判断)
// - フロントエンド単体でも安全側に倒しておく方が、多層防御として堅牢
// という2つの理由で、フロントエンド側でも同じ考え方の検証を行う
export function safeRedirectPath(path: string | null | undefined, fallback = "/tasks"): string {
  if (!path) return fallback;
  // 単一の "/" で始まり、"//"(protocol-relative URL)や "/\\"(一部ブラウザが
  // バックスラッシュをスラッシュとして正規化することを悪用するトリック)で始まらないことを要求する
  // これにより、後続でこの値をURLとして扱っても
  // 常に「現在のオリジン配下のパス」としてのみ解釈されることを保証する
  if (!path.startsWith("/")) return fallback;
  if (path.startsWith("//") || path.startsWith("/\\")) return fallback;
  return path;
}
