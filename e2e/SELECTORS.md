# E2E共通selector規約

Playwright/Cypress/Selenium(JS)、chromedp/go-rod/playwright-go(Go、CONTRACT.mdセクション18)の
計6種で同じ導線を検証するため、frontend実装側は以下の`data-testid`を必ず付与すること
(このファイルはfrontend実装との単一の約束事)

| data-testid | 要素 | 画面 |
|---|---|---|
| `login-email-input` | ローカル認証のメールアドレスinput | /login, /login/rsa |
| `login-password-input` | ローカル認証のパスワードinput | /login, /login/rsa |
| `login-submit-button` | ローカル認証の送信ボタン | /login, /login/rsa |
| `login-error` | ローカル認証失敗時のエラーメッセージ(`alert-danger`) | /login, /login/rsa |
| `login-rsa-link` | 「RSA版で試す」リンク(`/login/rsa`へ) | /login |
| `login-hmac-link` | HMAC版(`/login`)へ戻るリンク | /login/rsa |
| `login-keycloak-button` | 「Keycloakでログイン」ボタン(`/api/auth/login/keycloak`へ遷移) | /login, /login/rsa |
| `logout-button` | ログアウトボタン | ヘッダ(ログイン後全画面) |
| `task-list` | タスク一覧のルート要素(新実装 TaskList.tsx / 旧実装 legacy/TaskList.jsx 両方に付与) | /tasks |
| `task-row` | タスク一覧の各行(`data-task-name`属性にタスク名を入れる) | /tasks |
| `task-name-input` | タスク名input | /tasks(画面上部に常設のフォーム) |
| `task-status-select` | ステータスselect | /tasks |
| `task-finished-on-input` | 期限日input(date) | /tasks |
| `task-submit-button` | 登録/更新ボタン(新規作成/編集で共用の同一フォーム) | /tasks |
| `task-edit-button` | 行内の編集ボタン(`task-row`内。クリックすると上部フォームに値が入る、別画面遷移はしない) | /tasks |
| `task-delete-button` | 行内の削除ボタン(`task-row`内、確認ダイアログ即時許可でよい) | /tasks |
| `task-create-button` | 「タスクを登録」ボタン(modal版のみ、クリックでモーダルを開く) | /tasks(`frontend.task-create-ux=modal`時) |
| `task-create-modal` | 登録用モーダルのルート要素(内部に`task-name-input`等、上記と同じフォームのtestidを持つ) | /tasks(`frontend.task-create-ux=modal`時) |
| `task-modal-close` | モーダルの閉じるボタン | /tasks(`frontend.task-create-ux=modal`時) |
| `task-create-link` | 「タスクを登録」リンク(page版のみ、`/tasks/new`へ遷移) | /tasks(`frontend.task-create-ux=page`時) |
| `task-form-page` | 登録/編集専用ページのルート要素(内部に`task-name-input`等、上記と同じフォームのtestidを持つ) | /tasks/new, /tasks/:id/edit |
| `back-to-tasks-link` | 「一覧へ戻る」リンク | /tasks/new, /tasks/:id/edit |
| `login-passkey-button` | 「パスキーでログイン」ボタン(discoverable credential方式、CONTRACT.mdセクション22) | /login, /login/rsa |
| `passkey-register-button` | 「パスキーを登録」ボタン | /account(要ログイン) |
| `passkey-device-name-input` | 登録するパスキーの端末名(任意)input | /account |
| `passkey-register-success` | パスキー登録成功メッセージ(`alert-success`) | /account |
| `passkey-register-error` | パスキー登録失敗メッセージ(`alert-danger`) | /account |

統合レビューで判明: 実装は「登録画面」「編集画面」が別ルートに分かれておらず、
`/tasks`の画面上部に常設された1つのフォームを、新規作成と編集(行の「編集」ボタンクリックで値を流し込む)の両方で共用する単一ページ構成になっている
そのため`task-new-link`(新規作成画面への遷移)に相当する要素は存在せず、フォームへの入力→`task-submit-button`クリックだけで新規作成が完了する

【重要・後から修正】
旧実装(legacy/TaskList.jsx)は当初「一覧表示のみ」に機能を絞っていたが、
これは実務のStrangler Figの前提(旧実装はまだ現役の本番コードである以上、
新実装と機能面で同等でなければならない)と矛盾するという指摘を受け、旧実装にも新実装(TaskList.tsx)と同じ
`task-name-input`/`task-status-select`/`task-finished-on-input`/`task-submit-button`/`task-edit-button`/`task-delete-button`を持つフル機能を実装した
そのため、Feature Flag ON/OFFいずれの状態でも一覧/登録/更新/削除の一連のE2Eシナリオが同じセレクタで実行できる
両実装の違いはTypeScript/JavaScriptという実装言語のみ

## Task登録UX(CONTRACT.mdセクション19・19.7、`frontend.task-create-ux`)

Task登録のUIには、多値Feature Flag `frontend.task-create-ux` で切り替わる3パターンがある。
`task-name-input`等の**フォーム自体のtestidはinline/modal/pageの3パターンで完全に共通**
(TS実装・JS(legacy)実装間でも共通)なので、E2E側は「どの画面遷移を経てフォームへたどり着くか」
だけを分岐すればよい。

- `inline`(既定): `/tasks`画面上部に常設。フォームへ直接入力するだけで、追加の遷移は不要
- `modal`: `/tasks`画面に`task-create-button`が表示される。クリックすると`task-create-modal`内に
  フォームが開く。編集も同様にモーダルで行う(行の`task-edit-button`クリックでモーダルが開く)
- `page`: `/tasks`画面に`task-create-link`が表示される。クリックすると`/tasks/new`へ遷移し、
  `task-form-page`内にフォームが表示される。編集は`/tasks/:id/edit`。保存後は自動的に`/tasks`へ戻るが、
  `back-to-tasks-link`でキャンセルもできる

フラグの切り替えはadmin/go・admin/rails経由(またはMySQLへ直接
`UPDATE feature_flags SET default_variation='modal' WHERE flag_key='frontend.task-create-ux'`)で行う。
CIでの自動切替は無いため、`feature-flag.spec.ts`と同じ「事前に手動でDBを切り替えてから実行する」運用。

Keycloakログインフォーム(Keycloak標準テーマ)は `#username` / `#password` / `#kc-login` を使う
(Keycloakのデフォルトid、frontend側の対応不要)

## パスキー(WebAuthn、CONTRACT.mdセクション22)

対象はbffのローカル認証(HMAC/RSA)ユーザーのみ(Keycloak発行ユーザーは対象外)。discoverable
credential方式のため、ログイン時にメールアドレス等の入力は不要で`login-passkey-button`を
押すだけでよい。

E2E側では実機の生体認証を使わず、各フレームワークの仮想認証器機能で自動化する
(Playwright(JS)・chromedp・go-rod・playwright-goはCDPの`WebAuthn`ドメイン、Seleniumは
WebDriver仕様のVirtual Authenticator拡張)。設定は全フレームワークで統一している:
`protocol=ctap2`・`transport=internal`・`hasResidentKey=true`(discoverable credentialの
検証に必須)・`hasUserVerification=true`・`isUserVerified=true`・
`automaticPresenceSimulation=true`(Seleniumでは既定trueの`isUserConsenting`が相当)。

Cypressは仮想認証器に相当する公式APIが無いため対応を見送っている(`e2e/cypress/README.md`参照)。

## 環境変数(全フレームワーク共通)

| 変数 | 既定値 | 説明 |
|---|---|---|
| `FRONTEND_BASE_URL` | `http://localhost:5173` | frontend(Vite dev server)のベースURL |
| `E2E_USERNAME` | `general-user` | Keycloakテストユーザー名(bff/keycloak/realm-export.jsonの実際のユーザー名) |
| `E2E_PASSWORD` | `password` | Keycloakテストユーザーパスワード(realm-export.json記載の値) |
| `E2E_LOCAL_EMAIL` | `local-user@example.com` | ローカル認証(HMAC/RSA共通)のメールアドレス(マイグレーション`000009_seed_local_user`でseed) |
| `E2E_LOCAL_PASSWORD` | `password` | ローカル認証のパスワード |

統合レビューで判明:
当初案の`e2e-user`/`e2e-password`はbffが実装したrealm-export.json の実際のユーザー
(`general-user`/`admin-user`、パスワードは共に`password`)
と一致していなかったため、実際の値に修正した
管理者権限が必要なテストには`admin-user`/`password`を使う

## ログインフローの実際の挙動(CONTRACT.mdセクション16、ローカル認証追加後の現状)

未ログイン状態で保護ページ(`/tasks`等)へアクセスすると、`/login`(ローカル認証HMAC版のフォーム)へ
リダイレクトされる。Keycloakへは自動遷移しない。3つのログインURLがある。

- `/login`(既定): ローカル認証HMAC版のログインフォーム
- `/login/rsa`: 同じフォーム、ローカル認証RSA版(`login-rsa-link`から遷移可能)
- Keycloakでログインしたい場合は、`/login`または`/login/rsa`内の`login-keycloak-button`を明示的にクリックする

Keycloakボタンをクリックした後に表示されるKeycloak自身のログインフォーム(標準テーマ)は
`#username` / `#password` / `#kc-login` を使う(Keycloakのデフォルトid、frontend側の対応不要)。

## 既知の注意点

このE2E一式はCONTRACT.mdの契約のみを根拠に実装しており、実際のfrontend/bff実装との突き合わせ(data-testidの付け漏れ、Keycloakログイン画面のid、ポート番号等)は未実施
実装後に一度通しで実行し、セレクタのズレがあれば本ファイルとテストコード双方を合わせて修正すること

【重要・後から修正】
当初`BFF_BASE_URL`(既定`http://localhost:8080`)という名前・値でベースURLを設定していたが、
`/tasks`等の画面を実際に配信するのは bff ではなく frontend(Vite dev server, `:5173`)である

bffは`/api/*`しか持たないGinルーターのため、
`:8080`を起点に`page.goto("/tasks")`すると「404 page not found」になっていた
(統合レビューで発覚した実際のバグと同種)
`FRONTEND_BASE_URL`(既定`http://localhost:5173`)
に名称・既定値とも修正済み

## frontend-rails/without-bff(CONTRACT.mdセクション21・22.9)は対象外

このファイルのdata-testid規約は、React(`frontend/`)にのみ適用される約束事である。
`frontend-rails/without-bff`(:5174)は素のRails ERBビューで、data-testidを一切付与しておらず、
素朴なCSS idのみを持つ(実装時にこの規約を踏襲しなかった)。この差異を把握せず同じ規約が
使えるものとして実装すると、テストコードが軒並みタイムアウトするため明記しておく。

このアプリ向けのe2eシナリオ(`*_frontend_rails_without_bff*`という命名で各フレームワークに
追加済み、CONTRACT.mdセクション22.9のパスキー機能)は、代わりに以下を使う:

| 要素 | セレクタ | 画面 |
|---|---|---|
| 「Keycloakでログイン」ボタン | 表示テキスト一致(`button_to`のため`id`無し) | /login |
| Keycloakログインフォーム | `#username` / `#password` / `#kc-login`(既存と同じ) | Keycloak標準テーマ |
| 「パスキーを登録」ボタン | `#passkey-register-button` | /account |
| 登録結果メッセージ | `#passkey-status`(テキストに「登録しました」を含むかで判定、成功/失敗で要素自体は分かれていない) | /account |
| 「パスキーでログイン」ボタン | `#passkey-login-button` | /login |
| ログイン方式の表示 | ページ本文に`ログイン方式: passkey`という文字列が含まれるかで判定(`session[:auth_mode]`をそのまま出力) | /welcome |
| ログアウト | `GET /logout`(config/routes.rbが手動確認用に明示的に許可、ボタンクリックの代わりに直接遷移してよい) | どの画面からでも |
