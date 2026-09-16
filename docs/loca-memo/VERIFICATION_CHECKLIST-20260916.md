# bff-gin Verification Checklist

環境構築後の動作確認チェックリスト(bff-gin 検証ランブック v10 の内容を Markdown 化したもの)。

- 上から順に進めるのを推奨するが、セクション単位で行き来してもよい。
- `[SECURITY]` はセキュリティ回帰確認、`[NEW]` は直近の監査ラウンド(第4〜11ラウンド)で見つかった実バグの回帰確認項目。
- 多言語backend・frontend-rails関連は既定では未起動のオプション構成。「追加構成」と付記された項目のみ該当プロセスの起動が必要。

## 認証情報・URLリファレンス(コア構成)

| 項目 | 値 | 備考 |
|---|---|---|
| frontend | `http://localhost:5173` | React SPA |
| bff | `http://localhost:8080` | Go/Gin |
| backend REST v1 | `http://localhost:8090` | N+1あり・旧実装 |
| backend gRPC v2 | `localhost:9090` | Preload最適化・新実装 |
| backend 外部公開API(内部アドレス) | `http://localhost:8097` | gatewayが:8081からここへ振り分ける |
| keycloak | `http://localhost:8082` | 管理コンソール: admin / admin |
| mysql | `127.0.0.1:13306` | root / (パスワード無し) |
| redis | `127.0.0.1:16379` |  |
| swagger-ui | `http://localhost:18080` | 外部公開APIのTry it out(:8081を叩く) |
| admin/go | `http://localhost:8091` | Basic Auth: admin / password |
| admin/rails | `http://localhost:8092` | Basic Auth: admin / password |
| ローカルHMAC (/login) | `local-user@example.com / password` | role: general |
| ローカルRSA (/login/rsa) | `local-user@example.com / password` | 同一ユーザー |
| Keycloak (/login/keycloak) | `general-user / password` | role: general |
| Keycloak (/login/keycloak) | `admin-user / password` | role: management |

多言語backend(Rust/Scala×2/Rails)・ゲートウェイ・frontend-rails・bff-railsのポート/認証情報は、各セクション内のコマンド例に記載。

## 目次

1. [環境構築(コア構成)](#1-環境構築コア構成)
2. [ログイン(3方式)](#2-ログイン3方式)
3. [Task機能のCRUD](#3-task機能のcrud)
4. [Task登録UXの3パターン(inline/modal/page)](#4-task登録uxの3パターンinlinemodalpage)
5. [ラベル機能のCRUD](#5-ラベル機能のcrud)
6. [Feature Flagの切り替え(実際に挙動が変わることの確認)](#6-feature-flagの切り替え実際に挙動が変わることの確認)
7. [Admin画面: Feature Flag管理(Go / Rails比較)](#7-admin画面-feature-flag管理go-rails比較)
8. [Admin画面: ユーザー管理(Go / Rails比較)](#8-admin画面-ユーザー管理go-rails比較)
9. [パスキー(WebAuthn)](#9-パスキーwebauthn)
10. [セキュリティ回帰確認](#10-セキュリティ回帰確認)
11. [backend多言語比較(Go / Rust / Scala×2 / Rails)](#11-backend多言語比較go-rust-scala×2-rails)
12. [外部公開APIゲートウェイ(Go製 / nginx製)](#12-外部公開apiゲートウェイgo製-nginx製)
13. [frontend-rails(without-bff / with-bff)+ bff-rails](#13-frontend-railswithout-bff-with-bff+-bff-rails)
14. [各種ログの確認](#14-各種ログの確認)
15. [Redisの中身](#15-redisの中身)
16. [Keycloakのログ・管理画面](#16-keycloakのログ管理画面)
17. [外部公開API(BFF非経由)](#17-外部公開apibff非経由)
18. [backend(Go)内でのORM比較(GORM / bob)](#18-backendgo内でのorm比較gorm-bob)
19. [E2E自動テスト(手動確認の代わり・裏付けとして)](#19-e2e自動テスト手動確認の代わり裏付けとして)

## 1. 環境構築(コア構成)

_Docker起動 → マイグレーション → 各プロセス起動_

- [ ] (確認) 今回の環境構築やり直しで、DBデータ自体の変更・再作成は不要
  - note: 2026-09-12〜13の一連の修正(v10)はアプリケーションコードレベルのみで、新しいmigrationファイルは1つも追加していない(最新は引き続き`000015`、15ファイルのまま)。既存のMySQLデータボリューム(`bff-gin-mysql-data`)をそのまま使う場合、マイグレーションの再実行やデータの初期化は不要。完全に作り直す場合(`docker compose down -v`等)は、下記の手順通り`000015`まで適用すれば以前と同じ状態になる。
- [ ] Docker Desktopが起動している
- [ ] コンテナ群(mysql/redis/keycloak/swagger-ui)を起動し、全てhealthyになる
  - expect: 4サービスとも STATUS が (healthy)
  ```
  docker compose up -d --wait mysql redis keycloak swagger-ui
  docker compose ps
  ```
- [ ] MySQLのマイグレーションを実行する(既存データを使う場合もこのコマンド自体は実行して問題ない、未適用分だけ適用されて冪等)
  - expect: マイグレーション(up)が完了しました。(000015まで適用されるはず、今回追加分は無し)
  ```
  cd backend
  go run ./cmd/migrate up
  ```
- [ ] backendを起動する(REST v1 / gRPC v2 / 外部公開APIの3ポート)
  ```
  cd backend
  go run ./cmd/server
  ```
  - note: 既に古いプロセスが残っていないか(ポート8090/9090/8097)事前に確認しておくと事故が少ない。外部公開APIの待受ポートは:8097に変更されている(:8081はgatewayが占有する、後述のセクション参照)。コード変更後は必ず再起動すること(ステールプロセス問題は既知の落とし穴)。
- [ ] bffを起動する
  ```
  cd bff
  go run ./cmd/server
  ```
- [ ] frontendを起動する
  ```
  cd frontend
  npm run dev
  ```
- [ ] admin/go を起動する
  ```
  cd admin/go
  go run ./cmd/server
  ```
- [ ] admin/rails を起動する(初回は bundle install)
  ```
  cd admin/rails
  bundle install   # 初回のみ
  bin/rails server -p 8092
  ```

## 2. ログイン(3方式)

_ローカルHMAC・ローカルRSA・Keycloak、それぞれログイン〜ログアウトまで_

- [ ] /login (ローカルHMAC) で local-user@example.com / password でログインできる
  - expect: タスク一覧画面へ遷移する
- [ ] ログアウトできる(HMACセッション)
  - note: ログアウト後、同じCookieのままAPIを叩くと401になることも合わせて確認するとよい(ブラウザの再読み込みでログイン画面に戻ることで代用可)。
- [ ] /login/rsa (ローカルRSA) で同じユーザーでログインできる
- [ ] ログアウトできる(RSAセッション)
- [ ] /login の「Keycloakでログイン」ボタンから general-user でログインできる
  - expect: Keycloakのログイン画面が一度表示され、成功後タスク一覧へ戻る
- [ ] ログアウトできる(Keycloakセッション、RP-Initiated Logout)
  - note: ログアウト後にKeycloak側のログイン画面が表示されれば、Keycloak側のセッションも正しく切れている。
- [ ] admin-user(role: management)でもKeycloakログインできる
- [ ] 誤ったパスワードで/loginを試すと、エラーメッセージが表示されログインできない
  - expect: 「メールアドレスまたはパスワードが正しくありません」
- [ ] `[SECURITY]` /login?redirect=http://evil.example.com/ のような外部URLを指定しても、ログイン後に外部サイトへ遷移しない
  - note: Open Redirect対策の回帰確認(frontend/LoginForm.tsx・bff Callback両方に多層防御を追加済み)。相対パス以外はfrontendのトップ等へフォールバックするはず。

## 3. Task機能のCRUD

_作成・一覧・編集・削除、新旧実装の両方(inline版)_

- [ ] タスクを新規作成できる(名前・ステータス・期限・ラベル)
  - expect: 「タスクを作成しました」の成功メッセージ
- [ ] 作成したタスクが一覧に表示される
- [ ] タスクを編集(更新)できる
  - expect: 「タスクを更新しました」
- [ ] タスクを削除できる
  - expect: 「タスクを削除しました」、一覧から消える
- [ ] タスク名を21文字以上入力すると、フォームまたはサーバー側で弾かれる
- [ ] タスクが0件のとき、一覧が崩れず表示される
- [ ] `[SECURITY]` タスク名に `<img src=x onerror=alert(1)>` のような文字列を入力しても、実行されずそのまま文字列として表示される
  - note: XSS対策の回帰確認(Reactの自動エスケープに依存)。20文字制限内に収まるペイロードで確認する。
- [ ] `[SECURITY]` `[NEW]` 存在しないタスク名(20文字超え等)でわざとエラーを起こすと、エラーメッセージが読める日本語文になっている(生のJSON文字列がそのまま表示されない)
  - note: 【重大な表示バグの回帰確認】以前は`apiFetch`がエラーレスポンスのbodyをJSONパースしておらず、backendが返す`{"error":"...","message":"..."}`という生のJSON文字列がそのままエラーメッセージとして表示されていた(全画面・全エラーパスに影響)。コンポーネントテストが`api.ts`をモックしていたため長らく発見されなかった。今は修正済みで、人間が読める文言(例:「タスク名は20文字以内で入力してください」)が表示されるはず。
- [ ] `[SECURITY]` `[NEW]` 削除ボタン押下直後(通信中)に別の行の編集・削除を続けて行っても、最終的な一覧が古い応答で上書きされない
  - note: stale-response上書きバグの回帰確認。以前は`reload()`の応答順序が保証されておらず、削除操作の完了待ち中に別の操作を先に完了させると、直前の操作結果がエラー表示無く画面から消えることがあった(TS新実装・legacy JS版とも修正済み)。素早く連続操作しても最終的に正しい一覧に収束することを確認する。

## 4. Task登録UXの3パターン(inline/modal/page)

_多値Feature Flag frontend.task-create-ux。TS/JS実装(tasks-ts-rewrite)とは独立した軸_

- [ ] admin画面で frontend.task-create-ux を編集すると、既存3フラグ(ON/OFFトグル)とは違い inline/modal/page の3択selectとして表示される
- [ ] frontend.task-create-ux を modal に切り替える
- [ ] (modal) 一覧に「タスクを登録」ボタンが表示され、クリックするとモーダルでフォームが開く
  - expect: 作成成功でモーダルが閉じ、一覧に反映される
- [ ] (modal) 行の「編集」ボタンもモーダルで開き、値が入っている
- [ ] (modal) モーダルを閉じても一覧の裏側の状態は崩れない
- [ ] `[SECURITY]` `[NEW]` (modal、キーボードのみで操作) モーダルを開くとフォーカスがモーダル内に移り、Tabキーで背後の一覧要素へは抜けない。閉じると「タスクを登録」ボタンへフォーカスが戻る
  - note: 【a11yの実バグ回帰確認】以前はモーダルが素の`<div>`実装で、開閉時のフォーカス管理が一切無く、キーボードのみで操作するユーザーが閉じた後に操作を続けられなくなっていた(TS新実装・legacy JS版とも修正済み)。マウスを使わずTab/Escapeキーだけで一連の操作ができることを確認する。
- [ ] frontend.task-create-ux を page に切り替える
- [ ] (page) 一覧に「タスクを登録」リンクが表示され、クリックすると /tasks/new へ遷移する
  - expect: 作成成功で自動的に /tasks へ戻る
- [ ] (page) フォーム画面(/tasks/new・/tasks/:id/edit)に「一覧へ戻る」リンクがあり、保存せずに一覧へ戻れる
  - note: 以前「リンク/ボタンから遷移した先に戻る手段が無い画面」バグが見つかった箇所。必ず確認すること。
- [ ] frontend.tasks-ts-rewrite を ON にした状態でも、上記modal/page双方が同様に動く
  - note: legacy(JavaScript)実装側にも同じ3パターンが独立して実装されている。TS/JS × inline/modal/page の6通りの組み合わせが全て動くことが理想。
- [ ] 確認後、frontend.task-create-ux を inline に戻す(既定値)

## 5. ラベル機能のCRUD

_/labels 画面での作成・編集・削除_

- [ ] ラベルを新規作成できる
  - expect: 「ラベルを作成しました」
- [ ] ラベルを編集できる
  - expect: 「ラベルを更新しました」
- [ ] ラベルを削除できる
  - expect: 「ラベルを削除しました」
- [ ] 作成したラベルがタスクのラベル選択(Select2)に表示され、タスクに紐付けられる
- [ ] `[SECURITY]` 使用中(いずれかのタスクに紐付いている)のラベルを削除しようとすると拒否される
  - note: 以前は使用中チェックが無く、削除するとtask_labelsに孤立行が残るバグがあった(修正済み)。422エラーになることを確認する。
- [ ] `[SECURITY]` `[NEW]` タスクのラベル選択で同じラベルを重複して送信しても(通常のUI操作では起きないはずだが)、タスク作成/更新が500エラーにならない
  - note: 【5言語中4言語に影響した実バグの回帰確認】同一リクエスト内に重複するlabel_id(例: [3,3,5])を送ると、Go・Rust・Scala(http4s)・Railsで生のDB制約違反エラー(500相当)になっていた(Scala(Pekko)のみ元から正しかった)。通常のSelect2 UIでは同じラベルを二重選択できない構造のため、UI操作だけでは再現しない(直接APIを叩いた場合の防御の確認、という位置づけ)。

## 6. Feature Flagの切り替え(実際に挙動が変わることの確認)

_admin画面でフラグを変更 → フロント/BFF/外部APIの挙動が変わることまで確認_

- [ ] frontend.tasks-ts-rewrite を admin画面でON→タスク画面が新実装(TypeScript)に切り替わる
  - note: ブラウザのコンソールに `[feature-flag] frontend.tasks-ts-rewrite=true → 新実装...` のログが出る。OFFに戻すと旧実装(見出しが「旧実装 / JavaScript」)に戻る。
- [ ] bff.tasks-backend-v2 を admin画面でON→bffのログにv2(gRPC)へ振り分けたログが出る
  - note: 【変更】このフラグはbackend.task-protocolに統合され、bffはもう評価しない(admin画面には非推奨として残置、履歴保持のため)。実際の切り替えは次項のbackend.task-protocolで確認する。
- [ ] backend.task-protocol を admin画面でrest⇄grpcに切り替えると、bffのログに振り分け結果(implementation)が出る
  - expect: "language":"go","protocol":"grpc","implementation":"go:grpc" 等
  ```
  # bffのログ(標準出力)を確認
  # "backend.task-language/backend.task-protocolの評価結果によりTaskの実装を振り分け" の implementation を見る
  ```
- [ ] backend.external-tasks-pagination-v2 を admin画面でON→外部公開APIのレスポンス形状が変わる(offsetのpage/page_size→cursorのnext_cursor)
- [ ] backend.external-tasks-orm を admin画面でgorm⇄bobに切り替えても、外部公開APIのレスポンス形状は一切変わらない
  - note: 内部実装(GORM/bob)の入れ替えのみで、ワイヤー契約は完全に同一のはず。詳細は後述の「外部公開API」セクション参照。
- [ ] admin/goでの変更が、しばらく待つとbffのポーリング経由で反映される(即時ではない)
  - note: backendの GET /internal/v1/feature-flags/export をbffが定期ポーリングしている。反映まで数秒〜十数秒のラグがある。
- [ ] frontend.task-create-ux(3値)を admin画面で切り替えると、一覧画面の登録UIがinline/modal/pageに切り替わる

## 7. Admin画面: Feature Flag管理(Go / Rails比較)

_同じMySQLテーブルを2つの独立実装から操作する_

- [ ] `[GO+GIN]` admin/go の一覧画面(:8091)でフラグ一覧が表示される
  - note: 既存4種(frontend.tasks-ts-rewrite / bff.tasks-backend-v2(非推奨) / backend.external-tasks-pagination-v2 / frontend.task-create-ux)に加え、`backend.task-language`・`backend.task-protocol`・`frontend-rails.oidc-gem`・`backend.external-tasks-orm`の計8種が表示されるはず。
- [ ] `[GO+GIN]` admin/go でフラグのenabled/default_variationを変更できる
- [ ] `[GO+GIN]` admin/go の変更履歴(audit log)画面に変更が記録される
- [ ] `[RAILS]` admin/rails の一覧画面(:8092)でフラグ一覧が表示される(同じ8種)
- [ ] `[RAILS]` admin/rails でフラグを変更できる
- [ ] `[RAILS]` admin/rails の変更履歴画面に変更が記録される
- [ ] 片方(例: admin/go)で変更した内容が、もう片方(admin/rails)の画面をリロードすると反映されている
  - expect: 同じMySQLテーブルを直接見ているため、リロードだけで一致するはず
- [ ] Basic Auth無しでアクセスすると401になる(両方)

## 8. Admin画面: ユーザー管理(Go / Rails比較)

_新規作成・role変更・削除・パスキー登録状況。backend経由(直接DBではない)_

- [ ] `[GO+GIN]` admin/go の /users 一覧に既存ユーザーが表示される
- [ ] `[GO+GIN]` admin/go でローカル認証ユーザーを新規作成できる(name/email/password/role)
  - expect: 一覧に反映される
- [ ] 作成したユーザーで実際に /login からログインできる
  - note: 作成直後のメールアドレス・パスワードでそのままログイン可能なはず。
- [ ] `[GO+GIN]` admin/go でrole変更ができる
- [ ] `[GO+GIN]` admin/go で管理者(role: management)が1人しかいない状態にして削除・降格しようとすると拒否される
  - expect: 「最後の管理者」エラーメッセージが表示される(削除も降格もされない)
- [ ] `[SECURITY]` (上級者向け) 同じユーザーで2つのタブから同時に「最後の管理者」を削除しようとしても、片方しか成功しない
  - note: 以前はTOCTOU競合状態があり、2並行リクエストが両方成功し管理者が0人になり得た(SELECT ... FOR UPDATEで修正済み)。再現には素早い2並行操作が必要なため、無理に試さなくてもよい(該当のregression testはbackend側で自動確認済み)。
- [ ] `[GO+GIN]` admin/go でユーザーを削除できる(management以外)
- [ ] `[RAILS]` 同様に admin/rails の /users でも 一覧・作成・role変更・削除・最後の管理者ガードを確認する
- [ ] 重複するメールアドレスで作成しようとすると、分かりやすいエラーメッセージが出る(両方)
- [ ] ログイン中のユーザーを別ブラウザ/admin画面から削除すると、タスク/ラベル画面へ遷移しようとした際に自動的にログイン画面へ戻る
  - note: 401を受けてbff側のセッションが破棄され、自動的に/loginへリダイレクトされることを確認する。
- [ ] 一覧に「パスキー」列があり、パスキー登録済みユーザーは✅登録済み、未登録ユーザーは未登録と表示される(admin/go・admin/rails両方)
  - note: 後述の「パスキー(WebAuthn)」セクションでパスキーを登録した後、この一覧を再読み込みして確認するとよい。
- [ ] `[SECURITY]` `[NEW]` (上級者向け) パスキー登録済みのユーザーを削除しても、DBにwebauthn_credentialsの孤立行が残らない
  - note: 【実バグの回帰確認】ユーザー削除処理は`user_passwords`→`user_keycloaks`→`task_labels`→`tasks`の順にカスケード削除していたが、`webauthn_credentials`(パスキー機能追加時)がこの一覧に加えられておらず、パスキー登録済みユーザーを削除すると孤立行が永久に残っていた(修正済み)。確認する場合は`SELECT * FROM webauthn_credentials WHERE user_id = <削除したユーザーのid>;`が0件になることを見る(DBへの直接アクセスが必要な項目のため上級者向け)。

## 9. パスキー(WebAuthn)

_既存ユーザー(ローカル認証)への追加認証手段。discoverable credential方式_

- [ ] `[NEW]` (スコープの補足) このセクションはコア構成(React frontend + bff(Go) + backend)のみが対象
  - note: bff-rails側のパスキー登録・ログイン、およびbff(Go)とbff-rails間でのパスキー可搬性の確認は、後述の**「frontend-rails(without-bff / with-bff)+ bff-rails」セクション**にまとめてある(bff-railsは既定オフの追加構成のため)。**frontend-rails/without-bff独自のパスキー機能(Keycloakログインユーザー向け)**も同セクションに追加済み。
- [ ] /login (ローカルHMACまたはRSA) でログインする
  - note: パスキーは既存ユーザーへの追加の認証手段のため、まず何らかの方法でログインしておく必要がある(Keycloak発行ユーザーはこのパスキー機能の対象外)。
- [ ] アカウント画面(/account)で「パスキーを登録」を実行する
  - expect: ブラウザ標準のWebAuthn UI(Touch ID/Windows Hello/OSのパスキーダイアログ等)が表示され、登録成功のメッセージが出る
- [ ] ログアウトする
- [ ] /login 画面の「パスキーでログイン」ボタンから、メールアドレス入力無しでログインできる
  - expect: タスク一覧画面へ遷移する
  - note: discoverable credential方式のため、事前にメールアドレス等を入力する必要はない。ブラウザがそのサイト用に登録済みのパスキーを提示する。実機のGoogleパスワードマネージャー等クラウド同期パスキーでは、以前「Backup Eligible flag inconsistency」でこの手順が必ず失敗するバグがあった(修正済み、CONTRACT.mdセクション22.8)。
- [ ] admin画面(admin/go・admin/rails)のユーザー一覧で、このユーザーが「パスキー: 登録済み」と表示される
- [ ] (オプション、Chromeの場合)DevToolsのWebAuthnタブで仮想認証器を有効にすると、実機の生体認証無しで登録・ログインを試せる
  - note: e2eのGo/Playwright実装も同じCDP Virtual Authenticator機能を使って自動化している。ただしCDP仮想認証器はBackup Eligible=falseを既定にするため、上記の実機限定バグはこの方法では再現しない。

## 10. セキュリティ回帰確認

_3回のテスト監査(Open Redirect・Session Fixation・IDOR・TOCTOU等)で見つかった脆弱性の手動確認_

- [ ] `[SECURITY]` ブラウザのCookie値(session_id)を開発者ツールで別の値に書き換えると、以後のAPI呼び出しが401になる
- [ ] `[SECURITY]` ログイン中の状態で、開発者ツールのコンソールから X-CSRF-Token ヘッダ無しで /api/tasks へPOSTすると403になる
  ```
  fetch('/api/tasks', {method:'POST', headers:{'Content-Type':'application/json'}, body:'{}', credentials:'include'})
  ```
- [ ] `[SECURITY]` ログアウト後、ブラウザの「戻る」ボタンで直前のタスク一覧画面に戻ろうとしても、ログイン画面が表示される(古い一覧が一瞬でも見えない)
  - note: Cache-Control: no-store 等のセキュリティヘッダー追加による対策(bff/bff-rails両方)。
- [ ] `[SECURITY]` bff・bff-railsのレスポンスヘッダーに Cache-Control: no-store / X-Content-Type-Options: nosniff / X-Frame-Options: DENY が付与されている
  ```
  curl -sI http://localhost:8080/api/me | grep -iE "cache-control|x-content-type|x-frame"
  ```
- [ ] `[SECURITY]` 自分がログインしていない他ユーザーのタスクIDを直接指定してGET/PATCH/DELETEしても、404になる(自分のタスクとして操作できない)
  - note: IDOR(Insecure Direct Object Reference)対策の回帰確認。他ユーザーのタスクIDは推測が難しいため、厳密に試すのは難しいが、存在しないID(例: 999999)で404になることは確認できる。

## 11. backend多言語比較(Go / Rust / Scala×2 / Rails)

_Task CRUD(内部REST/gRPC)を5言語で実装。backend.task-language / backend.task-protocol で切り替え_

- [ ] 前提: Goのbackendが起動済みで、マイグレーションが000015まで適用されていること
  - note: Rust/Scala(http4s)/Scala(Pekko)/Railsはいずれも独自のマイグレーションを持たない。スキーマの正本は`backend/migrations`のみで、他4言語は同じMySQL(bff_gin_development)を読み書きするだけ。docker composeのmysql/keycloakも先に起動しておくこと。
- [ ] `[RUST]` `[NEW]` 追加構成: Rust backendを起動する(REST :8093 / gRPC :9093 / 外部公開API :8098)
  ```
  cd backend-rust
  LOG_LEVEL=debug cargo run
  ```
  - note: 初回はcrate(Rustのパッケージ)のダウンロードとコンパイルに数分かかる(2回目以降は速い)。`LOG_LEVEL`はこのプロジェクト独自の環境変数名(Rustでよく使われる`RUST_LOG`ではない)で、tracingクレートのログレベルを制御する。REST・外部公開API・gRPCの全てにリクエスト単位のログを追加済み(REST/外部APIは`axum::middleware::from_fn`、gRPCは`LoggingTaskGrpcService`という、tonic生成traitを直接ラップするデコレータ)。gRPCも実際の`tonic::Status::code()`(成否)まで正確に記録する。
- [ ] `[SCALA]` `[NEW]` 追加構成: Scala(http4s) backendを起動する(REST :8094 / gRPC :9094 / 外部公開API :8099)
  ```
  cd backend-scala-http4s
  sbt run
  ```
  - note: sbt(Scalaのビルドツール、Go modules+go buildに相当)は初回、依存ライブラリのダウンロードとプロジェクト全体のコンパイルで数分かかる。ログ設定ファイル(logback.xml)がプロジェクトに存在しないため、logbackの組み込みデフォルト(ルートロガーDEBUGレベル、コンソール出力)がそのまま使われる=追加操作なしで既にdebug相当のログレベルになっている。REST・外部公開API・gRPCの全てにリクエスト単位のログを追加済み(REST/外部APIは`org.http4s.server.middleware.Logger`、gRPCは自作の`io.grpc.ServerInterceptor`)。gRPCは実際のgRPCステータスコード(成否)まで正確に記録できる(2026-09-11以降、5言語全てが同じ精度)。
- [ ] `[SCALA]` `[NEW]` 追加構成: Scala(Pekko) backendを起動する(REST :8095 / gRPC :9095 / 外部公開API :8100)
  ```
  cd backend-scala-pekko
  sbt run
  ```
  - note: Scala(http4s)と同じ理由(logback.xml不在→デフォルトでDEBUGレベル)で追加のログレベル設定は不要。gRPC用のHTTP/2設定(`pekko.http.server.preview.enable-http2 = on`)も設定済みのため追加操作は不要。REST・外部公開API・gRPCの3つ全てにリクエスト単位のログがある(REST/外部APIは`RequestLogging.scala`、gRPCは`LoggingTaskGrpcService`という、pekko-grpc生成traitを直接ラップするデコレータ)。gRPCも実際の`io.grpc.Status`(成否)まで正確に記録する。既知の制約: REST/外部公開APIはルートに一切マッチしない404相当のパスを`status=rejected`と表示する(実際のステータスコードではない)。
- [ ] `[RAILS]` `[NEW]` 追加構成: Rails backendを起動する(REST :8096 / gRPC :9096 / 外部公開API :8101、3プロセス構成)
  ```
  cd backend-rails
  bundle install   # 初回のみ
  bundle exec puma -C config/puma.rb            # REST :8096
  bundle exec bin/grpc_server                     # gRPC :9096(別プロセス)
  bundle exec puma -C config/puma_external.rb    # 外部公開API :8101(こちらも別プロセス)
  ```
  - note: Goが1バイナリで3ポート全てbindするのに対し、Railsは**3プロセス**に分かれる(GRPC::RpcServerのブロッキングイベントループがPumaと同居できないため)。development環境は`config.log_level = :debug`が既定で設定済みのため、追加操作なしでSQLクエリまで含めた詳細ログが出る(Railsの標準ログ形式であり、Go/gatewayのようなJSON形式ではない点に注意)。REST/外部公開APIはこの標準ログで自動的にリクエスト単位のログが出る。gRPC(`bin/grpc_server`)には元々ログが無かったが、`Module#prepend`による横断的なログ(`grpc method=list_tasks status=ok duration_ms=3`形式)を追加済み。
- [ ] `[NEW]` 「どの言語が実際にリクエストを処理したか」を確認する方法
  ```
  # bffの標準出力を見る(全言語共通、まずこれを見る)
  # "backend.task-language/backend.task-protocolの評価結果によりTaskの実装を振り分け"
  # というログ行の language / protocol / implementation フィールドを見る
  # 例: "language":"rust", "protocol":"rest", "implementation":"rust:rest"
  ```
  - note: 各言語は互いに重複しないポートを専有している(Rust REST=:8093等)ため、bffがそのURLへ実際に接続できて200が返ってきたのであれば、対象の言語プロセスが起動していなければそもそも接続自体が失敗する(connection refusedやタイムアウトになり、他の言語が代わりに答えることはあり得ない)。2026-09-11時点で5言語×3種類(REST/外部公開API/gRPC)=15パターン全てにリクエスト単位のログが入った(下記参照)ため、bffのログとbackend自身のログの2箇所で二重に確認できる。
- [ ] `[NEW]` backend自身のログでもリクエスト単位に確認できる(5言語×REST/外部公開API/gRPCの15パターン全てに対応済み)
  ```
  # Go: JSON形式(method/path/status/duration_ms)。REST/外部公開API/gRPC全て対応、gRPCも実ステータス記録
  # 例: {"method":"GET","path":"/internal/v1/tasks","status":200,"duration_ms":3}
  
  # Rails: REST/外部公開APIは標準のRailsログ形式(自動)
  # Started GET "/internal/v1/tasks" for 127.0.0.1
  # Processing by Internal::V1::TasksController#index as JSON
  # Completed 200 OK in 12ms
  # gRPCは独自ログ(GRPC::BadStatus#codeで実ステータスコードを記録): grpc method=list_tasks status=OK(0) duration_ms=3
  
  # Rust: REST/外部公開API/gRPC全て対応、gRPCも実ステータス記録(tonic::Status::code())
  # method=GET path=/internal/v1/tasks status=200 duration_ms=3
  # grpc request method="list_tasks" status=Ok duration_ms=3
  
  # Scala(http4s): REST/外部公開API/gRPC全て対応、gRPCも実ステータス記録(io.grpc.Status.getCode)
  # INFO ... -- GET /internal/v1/tasks 200 OK 3ms
  # method=task.v1.TaskService/ListTasks status=OK duration_ms=3
  
  # Scala(Pekko): REST/外部公開API/gRPC全て対応、gRPCも実ステータス記録(io.grpc.Status)
  # INFO http.rest -- method=GET path=/internal/v1/tasks status=200 duration_ms=3
  # method=listTasks status=OK duration_ms=3
  ```
  - note: 2026-09-11、実機検証での「Rustにログが無いのは実務的に厳しい」という指摘を受け対応する中で、実はGo(外部公開API・gRPC)・Rails(gRPC)にもログの無い箇所があることが判明し、5言語×3種類=15パターン全てにログを追加(第1弾・第2弾)。さらに「正確に取れるのはGoだけか」という指摘を受け、Rust・Scala(Pekko)・Railsについても**実際のgRPCステータスコード(成否)まで正確に記録できるよう改善した(第3弾)**。**現在は5言語全てgRPCの実ステータスまで記録できる**(CONTRACT.mdセクション20.10・20.11)。実装方式は「ハンドラの型付き戻り値/例外を直接見る」(Go/Rust/Scala×2/Railsとも共通の考え方。HTTP/2トレーラーを直接覗く実装は、自分でハンドラを実装していない汎用ミドルウェア向けの手段であり今回は不要と判断)。Scala(Pekko)には既知の制約があり、REST/外部公開APIでルートに一切マッチしない404相当のパスは`status=rejected`と表示される(実際のステータスコードではない)。
- [ ] MySQLで backend.task-language を go→rust→scala-http4s→scala-pekko→rails の順に切り替え、そのつどfrontend(http://localhost:5173)からタスク一覧・作成・更新・削除を実際に操作し、同じ形状で動くことを確認する
  - expect: どの言語を選んでも、一覧の表示・作成成功メッセージ・更新成功メッセージ・削除後に一覧から消えることが同じように起こる(レスポンス形状も含めGoと完全に同じワイヤー契約のはず)
  ```
  docker compose exec mysql mysql -uroot bff_gin_development -e \
    "UPDATE feature_flags SET default_variation='rust' WHERE flag_key='backend.task-language';"
  # admin/go(http://localhost:8091)のFeature Flag編集画面から変更しても同じ
  ```
  - note: 反映まで最大10秒ほどのポーリング待ちがある(bff・backend自身とも10秒間隔でDBを再読込する固定値、環境変数での変更は不可)。curlで直接bffの/api/tasksを叩くこともできるが、セッションCookie+CSRFトークンが必須のため、frontendのブラウザ画面をそのまま使う方が簡単。
- [ ] backend.task-protocol を rest⇄grpc に切り替えても、5言語いずれでも同様に動く
  - note: 5言語×2プロトコル=10通りの組み合わせがある。全部は大変なので、最低限Go以外の1〜2言語で両プロトコルを確認すれば十分。gRPCへ切り替えた場合、bffのログのimplementationが`{言語}:grpc`になることも合わせて確認するとよい。
- [ ] `[SECURITY]` 他人のタスクIDを指定した削除で、Scala(http4s)・Scala(Pekko)ともラベル関連付けだけが消えてしまわないこと
  - note: 以前見つかったIDOR類似脆弱性の回帰確認(所有者チェック前にラベル関連を無条件削除していた)。存在しないtask idや他ユーザーのtask idでDELETEを試し、404になり自分のタスクのラベルも消えていないことを確認する。
- [ ] `[SECURITY]` `[NEW]` 期限(finished_on)を「今日の日付」に設定してタスクを作成すると、5言語のどのbackendでも一貫して受理される(日本時間の夜遅く〜深夜にかけて特に確認する価値がある)
  - note: 【重要なタイムゾーン不整合バグの回帰確認】「過去日付」判定に使う「今日」の計算基準が言語ごとにバラバラだった。Go・Scala(http4s)・Scala(Pekko)はサーバーのローカル/JVMデフォルトタイムゾーン、Rust・RailsはUTC基準だった。開発機がJST(UTC+9)のため、UTC 15:00〜23:59(日本時間で24:00〜翌8:59)の間は、同じ瞬間のリクエストで「今日」の日付が言語によって1日ズレて判定され、ある言語では受理される日付が別の言語では「過去日付」として拒否される不整合があった(修正済み、全言語UTC基準に統一)。この時間帯に5言語を切り替えながら同じ日付でタスク作成を試すと効果的に確認できる。
- [ ] 確認後、backend.task-language を go・backend.task-protocol を rest に戻す(既定値)

## 12. 外部公開APIゲートウェイ(Go製 / nginx製)

_:8081を占有し、backend.task-languageに応じて外部公開APIを振り分ける(常にGoへフォールバック)_

- [ ] `[GO+GIN]` 追加構成: Go製ゲートウェイを起動する(nginx製とは同時に起動しないこと、どちらも:8081を使うため)
  ```
  cd gateway/go
  go run .
  ```
- [ ] Keycloakでexternal-api-clientのトークンを取得し、:8081(ゲートウェイ)経由で/external/v1/tasksが呼べる
  - note: 既存の「外部公開API」セクションの手順と同じcurlで、ポートだけ:8081のまま(内部的にgatewayが:8097等へ転送する)。
- [ ] backend.task-languageをrust等に切り替えても、外部公開APIは引き続き200を返す
  - note: Rust/Scala/Rails側には外部公開APIも実装済みのため、フォールバックせず実際にその言語が応答する。ゲートウェイ自身のログ(標準出力、JSON)に「外部公開APIリクエストを振り分け」というログ行が出て、resolved_language・pathが確認できる。
- [ ] `[SECURITY]` `[NEW]` swagger-ui(http://localhost:18080)の「Try it out」から、ゲートウェイ経由(:8081)で /external/v1/tasks を実際に実ブラウザから叩ける
  - note: 【実バグの回帰確認】以前はGo製・nginx製ともゲートウェイにCORS設定が皆無で、Swagger UIからの「Try it out」(ブラウザによるクロスオリジンリクエスト、Authorizationヘッダ付きのためプリフライトが発生する)が実ブラウザでは失敗する状態だった(curlでの手動確認ではプリフライトを送らないため気づけなかった)。両ゲートウェイに`GATEWAY_ALLOWED_ORIGIN`(既定`http://localhost:18080`)によるCORS設定を追加済み。
- [ ] `[NGINX]` Go製ゲートウェイを停止し、nginx製ゲートウェイに切り替えて同じ確認を行う
  ```
  cd gateway/nginx
  ./start.sh
  ```
  - note: サイドカーのログに"upstream.confを書き換えました"→"nginxをreloadしました"が出ることを確認する。
- [ ] 確認後、backend.task-language を go に戻し、ゲートウェイプロセスを停止する

## 13. frontend-rails(without-bff / with-bff)+ bff-rails

_Railsで再現した2つの認証パターン。omniauth-openid-connect / openid_connect のgem切り替えも確認_

- [ ] `[RAILS]` 追加構成: frontend-rails/without-bff を起動する(BFFパターンを使わない構成)
  ```
  cd frontend-rails/without-bff
  bundle install   # 新規追加gem(webauthn/faraday/jwt)がある場合は必須
  bin/rails server -p 5174
  ```
- [ ] http://localhost:5174 でKeycloakログイン→「ようこそ、〇〇さん」画面が表示される
  - expect: Task一覧等のデータ表示は無し(スコープ上、ログイン確認のみ)
- [ ] welcome画面に「ログイン方式: keycloak」と表示される
  - note: CONTRACT.mdセクション22.9で追加。ログイン成功時にbackendへJITプロビジョニングが行われ、session[:user]["user_id"]・session[:access_token]・session[:auth_mode]が保存されるようになった(以前はKeycloakのsub/name/emailのみ)。
- [ ] `[SECURITY]` ログイン成功時、Railsのセッションが再発行されている(ログイン前のセッションIDが使い回されない)
  - note: Session Fixation対策の回帰確認(reset_session追加済み)。開発者ツールでログイン前後のセッションCookieの値が変わることを確認する。
- [ ] frontend-rails.oidc-gem を openid_connect に切り替えても、同様にログインできる
  ```
  docker compose exec mysql mysql -uroot bff_gin_development -e \
    "UPDATE feature_flags SET default_variation='openid_connect' WHERE flag_key='frontend-rails.oidc-gem';"
  ```
- [ ] `[RAILS]` `[NEW]` frontend-rails/without-bffでもパスキーを登録・ログインできる(要: 事前にKeycloakでログイン済みであること)
  - note: CONTRACT.mdセクション22.9で追加した新機能。welcome画面の「パスキーを登録する」リンクから `/account` へ進み「パスキーを登録」ボタンを押す→登録成功後ログアウト→`/login`画面の「パスキーでログイン」ボタンからメールアドレス入力無しでログインできる。backendの`webauthn_credentials`テーブルを共有するため、React+bff(Go)や後述のbff-railsで登録したパスキーもそのままログインに使える。
- [ ] パスキーのみでログインした場合、welcome画面に「ログイン方式: passkey」と表示される
  - note: パスキーログインはKeycloakを経由しないため、bff(Go)/bff-railsと同じ自前JWT(iss=bff-gin-local-hmac)がsession[:access_tokenに入る。この自前トークンでも/accountからの追加パスキー登録が問題なくできることも確認するとよい(backend側がiss=bff-gin-local-hmacのJWTも通常のaccess_tokenと同様に扱うため)。
- [ ] `[RAILS]` 追加構成: bff-rails + frontend-rails/with-bff を起動する(React+bffと同じ役割分担をRailsで再現)
  ```
  cd bff-rails
  bundle exec puma -C config/puma.rb   # :8102
  cd frontend-rails/with-bff
  bin/rails server -p 5175
  ```
- [ ] http://localhost:5175 でログイン→Task一覧が表示される(表示専用、作成/更新/削除は無し)
  - note: bff-railsがJITプロビジョニング・backendへのTask一覧取得を代行している。既存のReact+bffで作成したタスクと同じものが見えるはず。
- [ ] bff-railsでもパスキーを登録・ログインできる(要: 事前にKeycloakでログイン済みであること)
  - note: 登録はKeycloakログイン後に行う(既存ユーザーへの追加認証手段という原則はbff-railsも同じ)。パスキーのみでログインした後もTask一覧が取得できる(bff-railsが自前でbackend用JWTを発行するため)。ただしbff-railsは登録時にBackup Eligible/Backup Stateフラグをbackendへ送っていない(既知の未対応事項、CONTRACT.mdセクション22.8)。登録・ログインともbff-rails単体で完結する分には問題にならないが、bff-railsで登録したパスキーをbff(Go)経由でログインしようとすると、実機のクラウド同期パスキーでは同じ「Backup Eligible flag inconsistency」で失敗する可能性がある(逆方向、bff(Go)登録→bff-railsログインは問題ない)。frontend-rails/without-bffは新規実装のため、最初からBE/BSフラグを正しく送信している。
- [ ] React+bff(Go)・frontend-rails/without-bff・bff-railsのいずれで登録したパスキーも、互いにログインに使い回せる
  - note: backendの認証情報保存テーブルが共通のため。ただし上記の通り、bff-railsで登録したものをbff(Go)経由でログインする方向のみ、実機のクラウド同期パスキーで失敗しうる既知の制約がある。
- [ ] `[SECURITY]` 5分ほど放置(Keycloakのaccess_token失効)した後もTask一覧が取得できる(トークンリフレッシュが効いている)
  - note: 以前はbff-railsにトークンリフレッシュが無く、5分後に必ず502になるバグがあった(修正済み)。時間がかかるため必須ではないが、余裕があれば確認するとよい。
- [ ] frontend-rails.oidc-gem を openid_connect に切り替えても、bff-rails側も同様にログインできる
- [ ] 確認後、frontend-rails.oidc-gem を omniauth-openid-connect に戻す(既定値)、起動した追加プロセスを停止する

## 14. 各種ログの確認

_backend / bff / frontend、Feature Flag切り替えログを含む_

- [ ] backendの標準出力に、リクエストログ(method/path/status)がJSON形式で出ている
- [ ] backendのログに feature flag評価ログが出ている(flag/targeting_key/value)
- [ ] bffのログに feature flag評価ログ・振り分けログが出ている
- [ ] LOG_LEVEL=debug で起動すると、backendにGORMが発行したSQLログが追加で出る
  ```
  LOG_LEVEL=debug go run ./cmd/server
  ```
- [ ] `[NEW]` Goのgrpc v2・外部公開APIにも、リクエスト単位のログが出ている
  - note: 2026-09-11以前は内部REST(v1)にしかログが無く、外部公開API・gRPCは起動ログのみだった(「Goは元々ログが揃っている」という認識自体が誤りだった)。外部公開APIは`internal/handler/external/router.go`のrequestLogger、gRPCは`internal/grpcserver/server.go`のloggingUnaryInterceptorで追加。gRPCは最初から実際のgRPCステータス(`status.Code(err)`)まで記録できていた(grpc-goの`ChainUnaryInterceptor`が結果を直接渡してくれるため)。
- [ ] `[NEW]` 多言語backend(Rust/Scala×2/Rails)も、5言語×REST/外部公開API/gRPCの15パターン全てにログが揃った
  - note: 形式は言語ごとに異なる(Go=JSON・Rails=標準Railsログ+gRPCのみ独自形式・Rust/Scala×2=key=value形式)。**2026-09-11時点で、gRPCの実際の成否(ステータスコード)まで5言語全てが正確に記録できる**(いずれも「ハンドラの型付き戻り値/例外を直接見る」実装方式に統一、詳細はCONTRACT.mdセクション20.10・20.11)。詳細は「backend多言語比較」セクション参照。
- [ ] ブラウザの開発者ツール(コンソール)に、タスク一覧の新旧切り替えログ(console.info)が出ている

## 15. Redisの中身

_セッション・分散ロック・ログイン中の一時状態_

- [ ] redis-cliで接続できる
  ```
  docker compose exec redis redis-cli
  PING
  ```
- [ ] ログイン中にKEYSで session:{id} が存在することを確認する
  ```
  KEYS *
  ```
- [ ] session:{id} の中身(GET)に auth_mode が含まれ、ログイン方式(keycloak/local_hmac/local_rsa/passkey)と一致している
  ```
  GET session:<Cookieのsession_idの値>
  ```
  - note: auth_mode=passkeyのセッションも同じ形式で保存されているはず。frontend-rails/without-bffはRedisではなくRailsのCookieセッション(session[:auth_mode])で同等の情報を持つ点に注意(このセクションはbff(Go)・bff-rails向け)。
- [ ] TTLが設定されている(マイナスや-2ではない)
  ```
  TTL session:<session_idの値>
  ```
- [ ] ログアウト後、そのsession:{id}キーが消えている

## 16. Keycloakのログ・管理画面

_docker compose logs、管理コンソールでの設定確認_

- [ ] Keycloakのログにrealmインポート成功のログが出ている
  - expect: "Imported realm training" 等
  ```
  docker compose logs keycloak | tail -30
  ```
- [ ] 管理コンソール(http://localhost:8082)に admin / admin でログインできる
- [ ] realm "training" が存在し、クライアント bff-gin / external-api-client / frontend-rails / bff-rails が登録されている
  - note: frontend-rails・bff-railsは今回追加したクライアント(いずれもconfidential)。
- [ ] bff-gin・bff-rails・frontend-rails クライアントに oidc-audience-mapper(aud=backend)が設定されている
  - note: frontend-railsクライアントは元々このマッパーを持っていたが、CONTRACT.md上「無し」と誤記載されていた(セクション22.9のパスキー対応調査で発覚・訂正)。追加のKeycloak設定変更は不要。
- [ ] Users画面に general-user / admin-user が存在する
- [ ] Sessions画面で、実際にログインした際にアクティブセッションが増えることを確認できる

## 17. 外部公開API(BFF非経由)

_Client Credentials Grant、swagger-uiからも試せる、:8081はgateway経由。GORM/bobの切り替えも含む_

- [ ] swagger-ui(http://localhost:18080)が開ける
- [ ] Keycloakのトークンエンドポイントでaccess_tokenを取得できる
  ```
  curl -X POST http://localhost:8082/realms/training/protocol/openid-connect/token \
    -d grant_type=client_credentials \
    -d client_id=external-api-client \
    -d client_secret=<realm-export.jsonの値>
  ```
- [ ] 取得したトークンで GET /external/v1/tasks が呼べる(:8081、gateway未起動でも直接backendの:8097へ向ければ確認可能)
  ```
  curl http://localhost:8081/external/v1/tasks?user_id=1 \
    -H "Authorization: Bearer <access_token>"
  ```
- [ ] トークン無しで叩くと401になる

## 18. backend(Go)内でのORM比較(GORM / bob)

_外部公開APIのTask一覧取得を、backend.external-tasks-orm フラグでGORM/bob(https://github.com/stephenafamo/bob)に切り替え_

- [ ] backend.external-tasks-orm を admin画面またはDBで bob に切り替える
  ```
  docker compose exec mysql mysql -uroot bff_gin_development -e \
    "UPDATE feature_flags SET default_variation='bob' WHERE flag_key='backend.external-tasks-orm';"
  ```
- [ ] GET /external/v1/tasks のレスポンスが gorm のときと完全に同じ形状・同じ内容で返る(offset/cursor両方のページング方式で)
  - note: backend.external-tasks-pagination-v2 と組み合わせて4パターン(offset×gorm/offset×bob/cursor×gorm/cursor×bob)全て確認できると理想。内部実装(ORM)の入れ替えのみでワイヤー契約は変わらないはず。
- [ ] backendの起動ログに、どちらのORM実装が使われたかの振り分けログが出ている
- [ ] ラベル付きのタスクでも、bob版で正しくラベルが取得できる(N+1にならない)
  - note: task_labelsテーブルには外部キー制約が無いため、bobの自動リレーション生成の対象外になっている(GORMのPreload相当の処理を手動実装している)。ラベル付きタスクで意図的に確認する価値がある。
- [ ] 確認後、backend.external-tasks-orm を gorm に戻す(既定値)

## 19. E2E自動テスト(手動確認の代わり・裏付けとして)

_JS 3種(Playwright/Cypress/Selenium)+ Go 3種(chromedp/go-rod/playwright-go)、計6種_

- [ ] Playwright が全て通る
  ```
  cd e2e/playwright
  npx playwright test
  ```
- [ ] Cypress が全て通る
  ```
  cd e2e/cypress
  npx cypress run
  ```
- [ ] Selenium が全て通る
  ```
  cd e2e/selenium
  npm test
  ```
- [ ] chromedp が全て通る(Go)
  ```
  cd e2e/chromedp
  go test ./... -v
  ```
- [ ] go-rod が全て通る(Go)
  ```
  cd e2e/go-rod
  go test ./... -v
  ```
- [ ] playwright-go が全て通る(Go)
  ```
  cd e2e/playwright-go
  go test ./... -v
  ```
- [ ] 6種いずれも、Task登録UXのmodal/page版シナリオ(task-create-ux関連)が含まれている
  - note: 実行中にfrontend.task-create-uxをDB上で一時的に切り替えるため、複数のe2eスイートを同時実行すると競合することがある。1つずつ順番に実行し、実行後はフラグがinlineに戻っていることを確認する。
- [ ] Playwright/Selenium/chromedp/go-rod/playwright-goの5種に、仮想認証器を使ったパスキー登録→ログインのシナリオが含まれている(コア構成のReact+bff(Go)フロー)
  - note: Cypressのみ、WebAuthn仮想認証器の第一級APIが無いため見送り(cypress/README.mdに理由を明記済み)。
- [ ] frontend-rails/without-bff独自のパスキー機能(22.9)のシナリオが、playwright/cypress/selenium/chromedp/go-rodの5種に含まれている
  - note: playwright-goのみ未追加(既知の残課題、READMEに明記)。Keycloakログイン→/accountでパスキー登録→ログアウト→パスキーのみでログイン、という一連の流れを確認する。
- [ ] backend.task-language(多言語backend切り替え)・backend.external-tasks-orm(GORM/bob切り替え)のシナリオが6フレームワーク全てに含まれている
  - note: 以前はこの2つのシナリオが手動確認のみだった(自動化されていなかった)ことが判明し、全フレームワークに追加した。task-languageはbackend-rustが実際に起動している必要があるため、未起動の場合は自動的にスキップされる設計になっている(chromedp/go-rod)。JS 3フレームワークはMySQLクライアントへの依存を避けるため、admin/goの画面をHTTP経由で操作する方式でFeature Flagを切り替える。
- [ ] `[SECURITY]` Feature Flagを切り替えるシナリオ(task-create-ux・task-language・external-tasks-orm)は、テストがアサーション失敗で途中終了しても、Flagが元の値に確実に戻る
  - note: 【信頼性監査で発見・修正】JS 3フレームワーク(playwright/cypress/selenium)のtask-language・external-tasks-ormシナリオは、以前はテスト本体の末尾でのみFlagを復元しており、途中で失敗するとFlagが変更されたまま以降の全テスト実行に影響が残る構造だった。`afterEach`/`after`フックへ確実な復元処理を配線済み(Go製3フレームワークは元から`t.Cleanup`で安全だった)。
- [ ] `[SECURITY]` 6種全てに、resilience系シナリオ(ネットワーク遅延・戻る/リロード・複数タブセッション共有・パスキー登録がパスワードログインを壊さない回帰確認)が含まれている
  - note: Cypressのみ複数タブシナリオを見送り(仕様上の制約、CONTRACT.mdセクション18.5参照)。
- [ ] `[SECURITY]` 6種全てに、security系シナリオ(セッションCookie改ざん・CSRFヘッダ欠落・XSS実地確認・ログアウト後の情報露出確認)が含まれている
  - note: この4シナリオはCypress含む6フレームワーク全てで実装できている(WebAuthn・複数タブのような構造的制約に該当しないため)。
