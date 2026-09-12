# e2e/playwright-go

CONTRACT.mdセクション18: `../playwright/`(JS版、`@playwright/test`)と全く同じシナリオを、
[playwright-go](https://github.com/playwright-community/playwright-go)(Go言語のPlaywrightバインディング)で実装したもの

## セットアップ

```sh
cd e2e/playwright-go
go mod tidy

# Playwrightのブラウザ本体(Chromium)を別途インストールする(初回のみ)
go run github.com/mxschmitt/playwright-go/cmd/playwright install --with-deps chromium
```

**重要**: importパスは`github.com/mxschmitt/playwright-go`であり、`github.com/playwright-community/playwright-go`
**ではない**(GitHub org自体は`playwright-community`だが、公開されているGoモジュールのパス宣言は作者名`mxschmitt`のまま)
`go get`を素直に`playwright-community`で叩くと
`module declares its path as: github.com/mxschmitt/playwright-go`というエラーになる
これは実際に手を動かして初めて分かった、地味だが引っかかりやすい点

## 実行

frontend(:5173)・bff(:8080)・backend(:8090)が起動済みであること(`../../README.md`参照)

```sh
go test ./... -v
```

ヘッドフル(ブラウザを表示しながら実行、デバッグ用)にしたい場合:

```sh
E2E_HEADED=1 go test ./... -v
```

## カバーしているシナリオ(`../SELECTORS.md`・JS版と同一)

1. 未ログイン状態で`/tasks`→ローカルログイン画面(HMAC版・既定)へリダイレクト
2. ローカル認証(HMAC版)でログイン→タスク一覧表示→ログアウト→ログイン画面に戻る
3. ローカル認証(RSA版)でログイン→タスク一覧表示
4. 誤ったパスワードでのログイン→エラーメッセージ表示
5. Keycloakでログイン→タスク一覧表示→ログアウト→ログイン画面に戻る
6. Task作成→一覧に反映→更新→一覧に反映→削除→一覧から消える(inline版、`task_crud_test.go`)

さらに、CONTRACT.mdセクション19・19.7のTask登録UX3パターンのうちmodal版・page版を`task_create_ux_test.go`で検証する
多値Feature Flag`frontend.task-create-ux`をMySQLへ直接接続して
一時的に切り替え、終了後は必ず既定値(`inline`)へ戻す(`feature_flag_helpers.go`の`setTaskCreateUX`)
接続先は環境変数`MYSQL_DSN`(既定`root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true`)

## JS版(Playwright)と比べての所感

**APIの対応関係は非常に近い。移植は3フレームワーク中いちばん素直だった**
`page.GetByTestId(...)`・`.Fill(...)`・`.Click()`・`.WaitFor()`・`.Filter(HasText: ...)`など、
JSの`@playwright/test`とほぼ1対1で対応するメソッドがあり、`helpers.ts`→`helpers.go`の
移植は機械的に進んだ(当然だが、同じPlaywright本体をラップしているだけなので、
オートウェイト(要素が操作可能になるまで自動で待つ)などの中核的な挙動もJS版と共通)

**大きく違うのは「テストランナー側の機能が無い」こと**
JSの`@playwright/test`は、Playwright本体(ブラウザ操作ライブラリ)と`expect().toHaveCount(0)`の
ようなテストランナー+アサーションライブラリがセットになっている
playwright-goは　**ブラウザ操作APIだけ**　を提供するコミュニティ製バインディングで、Goの`testing`パッケージ+
`t.Fatalf`/`t.Errorf`を素直に組み合わせるしかない
「要素が0件になるまで待つ」ようなポーリングを伴うアサーション(`task_crud_test.go`の`waitForRowGone`)は自分で書く必要があり、
この点はJS版より一段低レイヤーで、chromedp/go-rodに近い書き味になる

**ブラウザインストールの手間はJS版と同程度**
`npx playwright install`のGo版として`go run .../cmd/playwright install`が用意されており、体験としてはJS版と変わらない
(内部的にはJS版と全く同じPlaywrightドライバ・ブラウザバイナリをダウンロードして使っている)

**Goで書く動機**: バックエンド(bff/backend)が既にGoで書かれているプロジェクトであれば、
e2eもGoで統一でき、CIパイプラインの言語ランタイムを1つに絞れる、というのが最大の利点

逆に、Playwright自体が持つトレースビューア・コードジェネレータ(`playwright codegen`)といった
周辺ツールチェインの充実度はJS版の方が圧倒的に高く、そこはトレードオフになる

## パスキー(WebAuthn、CONTRACT.mdセクション18・22)

playwright-go(このバインディング)にもWebAuthn専用の高レベルAPIは無いため、
`page.Context().NewCDPSession(page)`で生のCDPコマンド(`WebAuthn.enable`・
`WebAuthn.addVirtualAuthenticator`)を`map[string]any`で組み立てて送る必要がある

chromedp/go-rodが型付きの構造体を渡せるのに対し、ここだけ生のマップを手で書く分、
唯一「CDPコマンド名・パラメータ名を文字列でタイプミスしても実行時までコンパイラが検出できない」フレームワークになった

```bash
go test ./... -run TestPasskeyRegisterAndLogin -v
```

## ネットワーク遅延・ブラウザ操作・複数タブ・パスキー登録の後方互換(resilience_test.go)

2回目のe2e監査で追加

「壊れやすいのに見落とされがちな、アプリの土台部分の挙動」を対象にする:
`/api/tasks`応答が遅い間のローディング表示(`page.Route()`)、ブラウザバック/リロードでの認証維持、
複数タブでのセッション共有・ログアウト伝播(`page.Context().NewPage()`で同一 BrowserContext内に第2タブ)、
パスキー登録後もパスワードログインが壊れていないことの回帰確認

**go-rod版との対比が興味深い所感**: go-rod版の同種テスト(`e2e/go-rod/resilience_test.go`)では、
同じブラウザ内の2つ目のタブへの操作がCDPレベルで繰り返しハングする問題に当たり、ログアウト伝播シナリオは断念してskipにした
playwright-goでは全く同じ流れ(第2タブを開き、
片方でログアウト→もう片方をリロードしてセッション失効を確認)が実機検証の範囲では何の問題も無く動いた
実際のPlaywrightドライバ(Node製、Playwright本家が長年かけて
複数ページ・複数コンテキストの多重化まわりを堅牢にしてきたもの)を挟んでいる分、
生のCDPクライアントを自前で書いているgo-rod/chromedpより複数タブの扱いが安定している、という違いが表れた形になった

## セキュリティ(security_test.go、CONTRACT.mdセクション23)

3回目のe2e監査(セキュリティ観点)で追加
`../playwright/tests/security.spec.ts`の4シナリオを移植

本家Playwrightドライバをそのまま操作するバインディングのため、`BrowserContext.Cookies()`/
`AddCookies()`・`Page.OnDialog()`・`Page.Evaluate()`のPromise自動await挙動まで含め、
JS版とほぼ1対1で書けた(6フレームワーク中もっとも素直に移植できた)

実機検証で1点だけ、`AddCookies`に`URL`と`Path`を両方指定すると`"Cookie should have either url or path"`で
拒否されることが判明し、`URL`のみ指定する形に直した(JS版のTypeScript型定義だけを見ていると気づきにくい、実際にAPIを呼んで初めて分かる制約)

```bash
go test ./... -run TestSecurity -v
```

## 多言語backend切り替えシナリオについて(2026-09追記)

- `passkey_frontend_rails_without_bff_test.go`: `frontend-rails/without-bff`(CONTRACT.mdセクション22.9)独自のパスキー登録・ログインを確認
- **既知の未対応**: `backend.task-language`(rust切り替え)・`backend.external-tasks-orm`(bob切り替え)のシナリオは、chromedp/go-rod/playwright/cypress/seleniumの5フレームワークには追加済みだが、本フレームワークには未追加(2026-09時点の既知の残課題)
