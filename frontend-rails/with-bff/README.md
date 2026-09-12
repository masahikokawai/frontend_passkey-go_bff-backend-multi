# frontend-rails/with-bff

既存の `frontend`(React)と同じ役割を持つ、**薄いクライアント**の比較実装
OIDCハンドシェイク・セッション管理・backendへのアクセスは全て専用BFFの
`bff-rails`(`../../bff-rails/`、ポート`:8102`)が担当し、
このアプリ自身は MySQL・Redis・backend のいずれにも直接接続しない

## 画面

- `GET /`: 「ログイン」リンク(`bff-rails`の`/api/auth/login`へフルページ遷移)と、ログイン後に表示されるTask一覧(表示専用、作成/更新/削除は無し)
- Task一覧はサーバーサイドレンダリングではなく、ブラウザ側のJS(`fetch(..., {credentials:"include"})`)が`bff-rails`の`/api/tasks`を直接呼び、結果をその場でDOMへ描画する
  - (既存 React の `features/tasks/api.ts` が `bff` へ fetch するのと全く同じパターン)
- 未ログイン(`bff-rails`が401を返す)の場合は「ログインしてください。」を表示する
- 「パスキーでログイン」ボタン(常時表示)・「パスキーを登録」ボタン(ログイン後に表示)を追加した
  - ブラウザ標準の`navigator.credentials.create()`/`.get()`を素朴に呼ぶだけで、追加ライブラリは使っていない
  - (base64url⇔ArrayBufferの変換だけ自前実装、CONTRACT.mdセクション22参照)
  - 詳細は`../../bff-rails/README.md`の「パスキー(WebAuthn)対応」を参照

## セットアップ・実行

```sh
cd frontend-rails/with-bff
bundle install
bin/rails server -p 5175
```

`bff-rails`(`:8102`)が別途起動している必要がある(`../../bff-rails/README.md`参照)

## 環境変数

| 変数 | 既定値 |
|---|---|
| `HTTP_ADDR` | `5175` |
| `BFF_RAILS_ORIGIN` | `http://localhost:8102` |

## テスト

```sh
bundle exec rspec
```

## Reactの`frontend`との比較所感

- ロジックの分量そのものは驚くほど少ない
  - (ログインリンク1つ + fetch1回 + DOM描画)
  - BFF パターンの利点である「クライアント側の実装が薄くなる」ことを、
  - Rails という普段サーバーサイドレンダリングに寄った選択をされがちなフレームワークで作ってもやはり体感できた
- ただし、Reactのような差分描画・状態管理の仕組みが無いぶん、DOM操作は素朴な`document.createElement`の羅列になった
  - 表示専用の一覧程度なら問題にならないが、編集機能まで持たせるなら React/Vue 等のフロントエンドフレームワークを併用したくなるだろうという実感を得た
- CORS(`credentials: include`でのクロスオリジンfetch)
  - React + bffの組み合わせと全く同じ設定の考え方で問題なく動いた
  - (bff 側でオリジンを許可し、Cookie に `SameSite=Lax` + `httponly`)
  - フロントエンドの実装言語が React/Rails のどちらでも、BFF との契約は変わらないことを確認できた
