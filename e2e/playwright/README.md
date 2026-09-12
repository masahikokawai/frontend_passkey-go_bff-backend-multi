# E2E (Playwright)

## セットアップ

```bash
cd training-go/bff-gin/e2e/playwright
npm install
npx playwright install --with-deps chromium
```

## 実行

事前にdocker composeでmysql/redis/keycloak/backend/bff/frontendを起動しておくこと
(詳細は `training-go/bff-gin/README.md`)

```bash
FRONTEND_BASE_URL=http://localhost:5173 \
E2E_USERNAME=general-user \
E2E_PASSWORD=password \
npm test
```

## Feature Flag切り替えテスト

Feature FlagはMySQLの`feature_flags`テーブルが正本(CONTRACT.mdセクション13、admin/go・admin/railsから編集)
admin画面等で`frontend.tasks-ts-rewrite`を切り替えてから(bffの再起動は不要、
ポーリング反映まで数秒〜十数秒待つ)、それぞれの状態で実行する

```bash
FLAG_STATE=on  npx playwright test tests/feature-flag.spec.ts
FLAG_STATE=off npx playwright test tests/feature-flag.spec.ts
```

### Task登録UX(3パターン、CONTRACT.mdセクション19・19.7)

同様に`frontend.task-create-ux`を`modal`/`page`に切り替えてから実行する
(既定の`inline`は`task-crud.spec.ts`でカバー済み)

```bash
npx playwright test tests/task-create-ux.spec.ts -g "モーダル版"
npx playwright test tests/task-create-ux.spec.ts -g "別ページ版"
```

## 選定理由

3種類のE2Eフレームワークのうち、Playwrightは自動待機(auto-wait)とトレース機能が標準搭載されており、最も安定してフレーキーになりにくい
そのため主要導線(認証・Task CRUD・Feature Flag)を最も手厚くここに実装している

## パスキー(WebAuthn、CONTRACT.mdセクション22)

`page.context().newCDPSession(page)`でCDPの`WebAuthn`ドメインを直接叩き、仮想認証器を登録してから実行する(実機の生体認証は不要)
Playwright 自体に WebAuthn 専用の高レベルAPIは無いため、Puppeteer のような一級市民サポートと比べると一段低レイヤーな書き味になる

```bash
npx playwright test tests/passkey.spec.ts
```

## Admin画面(admin/go・admin/rails)のシナリオ

【2回目のe2e監査で追加】これまでadmin/go(`:8091`)・admin/rails(`:8092`)は6フレームワーク
いずれのe2e対象にもなっていなかったため、Playwrightに最低限のシナリオを追加した
(`tests/admin.spec.ts`、両実装を1つのテストファイルでパラメタライズして検証する)

両実装ともテンプレートに `data-testid` が付与されていない
(admin/*はこのプロジェクトの他のフォークが並行して担当していたため、テンプレートへの新規付与はスコープ外とした)
そのため `getByRole`/`h1`/name属性ベースのロケータのみで組み立てている
実装時に以下が判明した:
- admin/goと admin/rails でFeature Flag編集フォームの`<select name="...">`の名前が異なる
  (admin/goは`default_variation`だが、admin/railsはRailsの`form_with(model:)`がモデル名で
  名前空間化するため`feature_flag[default_variation]`になる)
  両方にマッチするCSSセレクタ(`select[name="default_variation"], select[name$="[default_variation]"]`)で吸収した
- admin/goのユーザー新規作成フォームは`<label>`と`<input>`がfor/id関連付けされておらず、
  Playwrightの`getByLabel`では要素を発見できない(アクセシビリティ上の既知の制約
  admin/goのテンプレート自体はスコープ外のため未修正、name属性ベースのロケータで回避した)

```bash
npx playwright test tests/admin.spec.ts
```

## ネットワーク遅延・ブラウザ操作・複数タブ・認証手段の後方互換(`resilience.spec.ts`)

【2回目のe2e監査で追加】
1回目の監査では機能追加への追随(パスキー・admin画面)が中心だったため、今回は見落とされがちな土台部分の挙動を対象にした

- `page.route()`でAPI応答に人為的な遅延を上乗せし、「読み込み中...」表示を検証
- ブラウザの戻る操作・リロードでも認証状態とデータ取得が正しく機能することを確認
- 同じブラウザコンテキストで2タブを開き、セッションの共有(片方でログイン→もう片方でも
  認証済み)とセッション失効の伝播(片方でログアウト→もう片方はリロードでログイン画面に戻る)
  を確認
- パスキーを登録した後も、既存のパスワード認証(ローカルHMAC)が引き続き使えることを回帰確認

```bash
npx playwright test tests/resilience.spec.ts
```

なお、Cypress/Selenium/chromedp/go-rod/playwright-goの5フレームワークへは、
今回のラウンドでは移植していない(発見したギャップの再現・修正をPlaywrightで検証することを優先したため)
同じ観点の移植は今後の課題として残る

## セッション・CSRF・XSSのブラウザ実地確認(`security.spec.ts`)

【3回目のe2e監査で追加】
1・2回目は機能追加への追随・土台部分の挙動が中心だったため、
今回はCONTRACT.mdセクション2(認証・CSRF設計)が実際のブラウザ操作でも守られているかを直接確認するシナリオを追加した
単体テスト(bff/backend側)では「ロジックが正しいか」しか見えないが、
ここでは「実際のブラウザから叩いた時に本当にブロックされるか」を見る

- **セッションCookie改ざん耐性**: `session_id` Cookieの値を書き換えても、以後のAPI呼び出しが
  401になることを確認(推測攻撃・セッションハイジャックへの耐性)
- **CSRFトークン欠落時の拒否**: `X-CSRF-Token`ヘッダを意図的に付けずに状態変更リクエストを
  送ると403で拒否されることを確認(bff側ミドルウェア単体の裏付け、frontend実装の正しさに依存しない)
- **XSS実地確認**: タスク名に`<script>x</script>`(20文字制限内に収まる短いペイロード)を
  実際に入力し、(a) alert等のダイアログが一切発火しない、(b) DOM上に実際の`<script>`要素として
  挿入されない(テキストとしてエスケープ表示される)ことの両方を確認
- **ログアウト後の保護ページ再訪**: ログアウト後に`/tasks`へ再度アクセスすると、
  ログイン画面へ戻され、直前のセッションで見えていたタスク一覧が一切表示されないことを確認

```bash
npx playwright test tests/security.spec.ts
```

**見つかったセキュリティ上の懸念(修正はスコープ外、報告のみ)**:
bffのレスポンスに`Cache-Control: no-store`等の明示的なキャッシュ制御ヘッダが設定されていない
(`bff/internal/auth/handler.go`・`middleware.go`を確認したがCache-Control関連のコードは無い)

今回の Playwright 実地テストでは `goto` による再アクセスのみを確認しており、
ログアウト後は正しくログイン画面へ戻ることを確認できたが、**ブラウザのback-forward cache(bfcache)を実際に経由した「戻る」ボタン操作までは検証できていない**
(Playwrightの`page.goBack()`は実ブラウザのbfcache挙動を完全には再現しない場合がある)

bfcacheはページのJS実行状態ごと保存されるため、理論上は「ログアウト直後に戻るボタンを押すと、
再認証チェックが走る前の一瞬、直前のタスク一覧が画面に残って見える」余地が完全には排除できていない

本番相当の運用では、認証が絡む画面のレスポンスに`Cache-Control: no-store, no-cache`を
明示的に付与することを推奨する(この対応自体は今回のディレクティブのスコープ外のため実施していない)

## 多言語backend切り替えシナリオ(2026-09追記)

- `backend-task-language.spec.ts`: `backend.task-language`をrustへ切り替え、Task CRUDが引き続き動くことを確認(要`backend-rust`起動)
- `backend-external-tasks-orm.spec.ts`: `backend.external-tasks-orm`をbobへ切り替え、外部公開APIの応答形状がGORM時と同一であることを確認(`APIRequestContext`によるHTTPレベルのテスト、ブラウザ操作は行わない)
- `passkey_frontend_rails_without_bff.spec.ts`: `frontend-rails/without-bff`(CONTRACT.mdセクション22.9)独自のパスキー登録・ログインを確認(要`frontend-rails/without-bff`起動)
