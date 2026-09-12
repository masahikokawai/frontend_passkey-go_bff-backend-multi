# frontend (React + Vite)

## セットアップ

```
cd training-go/bff-gin/frontend
cp .env.example .env   # 必要ならBFFのオリジンを変更
npm install
npm run dev            # http://localhost:5173
```

BFF(bff-gin/bff)を別途 http://localhost:8080 で起動しておくこと
開発時は`vite.config.ts` の proxy 設定により `/api/*` へのリクエストがBFFへ転送され、
ブラウザからは同一オリジンに見える(Cookieの送受信を単純にするため)

## ディレクトリ構成とコンポーネント共有ルール

```
src/
  app/            # ルーティング定義
  shared/         # アプリ全体で共有してよいもの(hooks, api client, layout)
  features/
    tasks/        # TypeScript化対象。一覧/登録/更新/削除すべて .tsx
      legacy/     # Feature Flag切り替えデモ用に意図的に用意した旧JS実装
    labels/       # 当面 .jsx のまま(段階移行の対象外)
```

**コンポーネント共有ルール**: `features/<name>/` 配下のコンポーネントは、同じ `features/<name>/` 配下からしか import しない
他のfeatureや`src/app`から`features/tasks/TaskForm` を直接importするようなことはしない
featureをまたいで共有したいものは `src/shared/` に切り出す

## TypeScript適用方針

- `tsconfig.json` は `allowJs: true` とし、JS/TSファイルが混在できる
- `features/tasks/` のみ `.tsx`/`.ts` で書く(段階的移行の第一歩)
- 他のfeature(`labels`など)は当面 `.jsx` のまま
  - 将来的に同じ要領で1機能ずつTS化していく

## Feature Flagによる新旧切り替えデモ(Strangler Fig)

`features/tasks/TaskListSwitch.tsx` が、BFFから受け取った
`frontend.tasks-ts-rewrite` フラグの値に応じて、以下のどちらを表示するかを実行時に切り替える

- ON: `TaskList.tsx`(新実装、TypeScript、CRUDフル機能)
- OFF: `legacy/TaskList.jsx`(旧実装、JavaScript、一覧表示のみの縮小版)

このフラグの値自体はBFFの `GET /api/me` レスポンスに含まれ、`src/shared/hooks/useFeatureFlag.ts` 経由で参照する
**フロントエンドからFeature Flag providerへ直接アクセスすることはない**(BFFが評価結果だけを渡す集約パターン)

`legacy/TaskList.jsx` は実際に稼働していた過去のコードではなく、この切り替え機構を学習するためにあえて用意した重複実装
新実装のロールアウトが完了したと判断したら、
- `features/tasks/legacy/TaskList.jsx`
- `features/tasks/TaskListSwitch.tsx`
- bff側の `frontend.tasks-ts-rewrite` フラグ定義

をまとめて削除し、ルーティングは `TaskList.tsx` を直接指すように変更すること
(Release Toggleは恒久的に残してはいけない)

## 認証・CSRF

- `src/shared/hooks/useAuth.ts` がアプリ起動時に `GET /api/me` を呼ぶ
  - 401の場合は`src/shared/api/client.ts` の `apiFetch` が自動的に `/login?redirect=<path>` へ遷移する
- 状態変更系リクエスト(POST/PUT/PATCH/DELETE)
  - `csrf_token` Cookie の値を `X-CSRF-Token` ヘッダに複製して送る(Double Submit Cookie方式)

## 既知の確認事項(bff実装との整合待ち)

- ログアウト
  - `POST /api/auth/logout` を fetch で呼ぶ
  - レスポンスの`{ "redirectUrl": "..." }` を使って `window.location.href` で遷移する実装にした
  - (fetchでは302の外部リダイレクトを実際のブラウザナビゲーションにできないため)
  - bff側の実装がこの形状で返す想定になっているか、統合時に要確認

## パスキー(WebAuthn、CONTRACT.mdセクション22)

bffのローカル認証(HMAC/RSA)ユーザーへの追加の認証手段
discoverable credential(resident key)方式を採用しており、ログイン画面でメールアドレス等の事前入力は不要

- `src/shared/webauthn/passkey.ts`: `navigator.credentials.create()`/`.get()`を直接呼ぶだけの薄いラッパー
  - base64url⇔ArrayBufferの変換は自前実装せず、
  - モダンブラウザ(Chrome 126+/Safari 18+/Firefox 141+)が標準搭載する
  - `PublicKeyCredential.parseCreationOptionsFromJSON`/`parseRequestOptionsFromJSON`/`credential.toJSON()`
  - に任せている
- `features/auth/LoginForm.tsx`:
  - 「パスキーでログイン」ボタン(`data-testid="login-passkey-button"`)を追加
  - メールアドレスの入力欄は無く、ボタン1つでブラウザ/OSが利用可能なパスキーを提示する
- `features/auth/AccountPage.tsx`(新設、`/account`):
  - ログイン中ユーザーがパスキーを登録できる画面
  (`data-testid="passkey-register-button"`)
  - Keycloak 発行セッションで押すと bff 側がスコープ外として拒否する(422相当)ため、ローカル認証ユーザーのみ利用できる
- クロスデバイス認証
  - (QRコードをスマホで読み取り、スマホ側で生体認証すると、その場に居るPCブラウザでもログイン成功する、という体験)
  - ブラウザ/OS標準の hybrid transport 機能であり、`navigator.credentials.get()`を正しく呼ぶだけで自動的に提供される
  - frontend側でQR表示や端末間通信を実装する必要はない

**実装してみての所感**:
go-webauthn(bff側)と組み合わせて使う分には、frontend側は驚くほど薄く済んだ
最大の理由は`PublicKeyCredential`の`*FromJSON`/`toJSON()`という比較的新しい
ブラウザ標準APIのおかげで、以前は各プロジェクトが自前で書いていたbase64url変換コードが一切不要になったこと

一方で、ログイン前(`login/begin`・`login/finish`)は`apiFetch`が使えず
素の`fetch`を使う必要がある点は、既存のローカルログインフォームと同じ制約を踏襲する形になった
