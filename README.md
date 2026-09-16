# frontend_passkey-go_bff-backend-multi

go-gin(サーバーサイドレンダリング + セッションCookie認証)を、
`frontend(React) / bff(Go) / backend(Go)` の3層構成へ発展させたアプリケーション
認証はOIDC(Keycloak)に準拠し、**ブラウザにはJWTを一切渡さないBFFパターン**を採用

設計の詳細・確定事項は [`CONTRACT.md`](./CONTRACT.md) を正本として参照すること
アーキテクチャ図(PlantUMLソース+PNG)は [`docs/`](./docs/) 配下を参照(`docs/_old/`は過去バージョンのバックアップにつき参照専用)

## 目次

- [アーキテクチャ概要](#アーキテクチャ概要)
- [アーキテクチャ詳細](#アーキテクチャ詳細)
- [前提バージョン](#前提バージョンmac-m2m5)
- [セットアップ手順](#セットアップ手順)
- [確認手順](#確認手順)
- [ディレクトリ構成](#ディレクトリ構成)
- [各種ログの出力先](#各種ログの出力先)
- [関連ドキュメント](#関連ドキュメント)
- [その他、検討事項など](#その他検討事項など)

## アーキテクチャ概要

インタラクティブなアーキテクチャ図: [`docs/1_architecture/bff-gin-system.architecture.html`](docs/1_architecture/bff-gin-system.architecture.html)をブラウザで開くとコア構成を俯瞰できる

主要フローのインタラクティブなシーケンス図(`docs/`配下、詳細は各節を参照):

- ログイン(HMAC/RSA/Keycloak): [`docs/2_sequence-diagrams/bff-gin-login-flows.sequence.html`](docs/2_sequence-diagrams/bff-gin-login-flows.sequence.html)
- パスキー登録→ログアウト→パスキーのみログイン: [`docs/2_sequence-diagrams/bff-gin-passkey-flow.sequence.html`](docs/2_sequence-diagrams/bff-gin-passkey-flow.sequence.html)
- Task作成(bff→backend REST/gRPC振り分け): [`docs/2_sequence-diagrams/bff-gin-task-create.sequence.html`](docs/2_sequence-diagrams/bff-gin-task-create.sequence.html)
- Feature Flagの伝播(admin→MySQL→backend/bffポーリング→Browser): [`docs/3_feature-flag-dataflow/bff-gin-feature-flag-propagation.dataflow.html`](docs/3_feature-flag-dataflow/bff-gin-feature-flag-propagation.dataflow.html)
- Taskのステータス遷移(waiting/work_in_progress/completed、削除): [`docs/4_lifecycle-diagrams/bff-gin-task-lifecycle.lifecycle.html`](docs/4_lifecycle-diagrams/bff-gin-task-lifecycle.lifecycle.html)
- セッションの状態遷移(ログイン→リフレッシュ→ログアウト/失効): [`docs/4_lifecycle-diagrams/bff-gin-session-lifecycle.lifecycle.html`](docs/4_lifecycle-diagrams/bff-gin-session-lifecycle.lifecycle.html)
- Architecture Delta(コア構成 vs 多言語backend込み構成の差分): [`docs/5_architecture-delta/bff-gin-core-vs-multilang.architecture-delta.html`](docs/5_architecture-delta/bff-gin-core-vs-multilang.architecture-delta.html)

<details>
<summary>テキスト版の詳細(クリックで展開)</summary>

```
ブラウザ(React, HttpOnly Cookieのみ保持)
   │ HTTPS + Cookie + X-CSRF-Token
   ▼
bff(Go/Gin) ── OIDC(Authorization Code+PKCE) ──> Keycloak
   │  Redis(session:{id} → Access/Refresh/ID Token, lock:session:{id})
   │  REST v1 or gRPC v2 (Feature Flag: backend.task-protocol)
   ▼
backend(Go、private network限定) ── JWKS検証 ──> Keycloak
   │
   ▼
MySQL 8.0
```

- **v1 (REST)**: N+1 クエリの実装「旧」
- **v2 (gRPC)**: `Preload` で N+1 を解消し、cursor ページングの実装「新」
- backend が `backend.task-protocol` フラグで v1/v2 を切り替える
  - さらに `backend.task-language` フラグで Go/Rust/Scala(http4s)/Scala(Pekko)/Rails の5言語実装を切り替えられる(多言語比較、後述)
- frontendは`features/tasks/`のみTypeScript化
  - `frontend.tasks-ts-rewrite` フラグで新旧コンポーネントを切り替える
- 認証はKeycloak(OIDC)だけでない
  - 比較用に**bff自身がJWTを発行するローカル認証(HMAC版/RSA版)**
  - 両方式共通の追加認証手段として**パスキー(WebAuthn)**も用意
  - ログインURL(`/login`/`/login/rsa`/`/login/keycloak`)で切り替えられる(詳細は後述の「ローカル(非Keycloak)認証」参照)

**ローカル開発でのプロセス配置**:
KeycloakのAuthorization Code Flow はブラウザを直接 Keycloak へリダイレクトするため、
コンテナ内部向けホスト名とブラウザが到達できるホスト名が一致している必要がある
そのため `mysql`/`redis`/`keycloak`/`frontend`/`swagger-ui`は`docker compose` でコンテナ化する一方、
`backend`/`bff` はコンテナ化せずホスト上で直接`go run`する(下記手順の通り)
`docs/c4_container_v2.puml` の注記も参照

**コア構成のプロセス一覧**:

| プロセス | 既定ポート | URL | 実行方法 |
|---|---|---|---|
| frontend | 5173 | http://localhost:5173 | ネイティブ(`npm run dev`) |
| bff | 8080 | http://localhost:8080 | ネイティブ(`go run`) Keycloakのredirect_uriがこの値を前提にしている |
| backend REST v1 | 8090 | http://localhost:8090 | ネイティブ(`go run`) bffの8080と衝突するため8080ではなく8090にした |
| backend gRPC v2 | 9090 | localhost:9090(gRPC、ブラウザから直接は使わない) | ネイティブ(`go run`) |
| backend 外部公開API(内部アドレス) | 8097 | http://localhost:8097 | ネイティブ(`go run`) 公開ポート:8081は下記gatewayが占有するため、backend自身はgatewayの転送先である8097で待ち受ける |
| keycloak | 8082(→コンテナ内8080) | http://localhost:8082 | docker compose |
| mysql | 13306(→コンテナ内3306) | 127.0.0.1:13306(MySQLプロトコル) | docker compose |
| redis | 16379(→コンテナ内6379) | 127.0.0.1:16379(Redisプロトコル) | docker compose |
| swagger-ui | 18080(→コンテナ内8080) | http://localhost:18080 | docker compose |
| admin/go | 8091 | http://localhost:8091 | ネイティブ(`go run`) Basic Auth必須 |
| admin/rails | 8092 | http://localhost:8092 | ネイティブ(`rails s -p 8092`) Basic Auth必須 |

多言語backend(Rust/Scala×2/Rails)・ゲートウェイ・frontend-rails・bff-railsは既定では未起動の追加構成のため、上記一覧には含めていない
ポート・起動コマンドはそれぞれ「[確認手順](#確認手順)」の該当節を参照
**ログインURL(比較用に3方式、詳細は後述「ローカル(非Keycloak)認証」参照)**:

| 方式 | frontendのURL | 実際に送信される先(bff) |
|---|---|---|
| ローカル・HMAC版(既定) | http://localhost:5173/login | `POST http://localhost:8080/api/auth/login` |
| ローカル・RSA版 | http://localhost:5173/login/rsa | `POST http://localhost:8080/api/auth/login/rsa` |
| Keycloak(OIDC) | http://localhost:5173/login 内の「Keycloakでログイン」ボタン | `GET http://localhost:8080/api/auth/login/keycloak` |

**各URLでログインできるユーザー**(いずれも「マイグレーションを実行する」の`go run ./cmd/migrate up`実行時にseedとして投入済み):

| ログインURL | ユーザー(email/username) | パスワード | 備考 |
|---|---|---|---|
| `/login`(ローカルHMAC) | `local-user@example.com` | `password` | role: general `user_passwords`テーブルにseed(有効期限2099年) |
| `/login/rsa`(ローカルRSA) | `local-user@example.com` | `password` | 上記と**同じユーザー**(署名方式が違うだけで、認証されるユーザー・照合ロジックは共通) |
| `/login/keycloak` | `general-user` | `password` | role: general Keycloakのrealm-export.jsonにテストユーザーとして定義 |
| `/login/keycloak` | `admin-user` | `password` | role: management(管理者相当) |

**frontend-rails/without-bffのログインURL**(追加構成、上記の3方式とは別のRails単独実装、CONTRACT.mdセクション21・22.9):

| 方式 | URL | 備考 |
|---|---|---|
| Keycloak(OIDC) | http://localhost:5174/login 内の「Keycloakでログイン」ボタン | Railsアプリ自身がKeycloakと直接Authorization Codeフローを行う(bffを経由しない) ユーザーは上記の`general-user`/`admin-user`と共用(同じKeycloak realm) |
| パスキー | http://localhost:5174/login 内の「パスキーでログイン」ボタン | 上記Keycloakログインの後に http://localhost:5174/account で事前登録したパスキーが必要(discoverable credential、メールアドレス入力不要) backendの`webauthn_credentials`テーブルはbff(Go)・bff-railsと共有しており、どちらで登録したパスキーも使い回せる |

**ユーザー作成の入口パターン・role・その後の認証方式**:

| 作成の入口 | 作成されるユーザーの種別 | roleの決まり方 | 作成直後にログインできる方式 | 追加でパスキー登録できるか |
|---|---|---|---|---|
| `admin/go`(:8091)または`admin/rails`(:8092)で新規作成(Basic Auth必須) | ローカル認証ユーザー(name/email/passwordを画面で指定、backendの`users`+`user_passwords`テーブル) | 作成時に`general`/`management`を選択 | `http://localhost:5173/login`(ローカルHMAC)・`http://localhost:5173/login/rsa`(ローカルRSA) いずれも同一ユーザー | できる(React+bffでログイン後、アカウント関連画面からパスキー登録 CONTRACT.mdセクション22、対象はローカル認証ユーザーのみ) |
| Keycloakで初回ログイン(JITプロビジョニング、admin画面を経由しない) | Keycloak発行ユーザー(backendの`users`テーブルに自動作成、パスワードは持たない) | Keycloak側の`realm_access.roles`クレームに従う(`realm-export.json`定義で`general-user`→`general`、`admin-user`→`management`) | `http://localhost:5173/login`または`http://localhost:5174/login`の「Keycloakでログイン」ボタン(ローカルHMAC/RSAではログイン不可、パスワード自体を持たないため) | `http://localhost:5174/login`(frontend-rails/without-bff)経由、または`http://localhost:5175`(frontend-rails/with-bff、実体はbff-rails)経由でならできる(CONTRACT.mdセクション22.9) `http://localhost:5173`(React+bff)側はKeycloakユーザー向けパスキーは非対応(セクション22.1で意図的に対象外) |

補足: Keycloak自体へのユーザー追加(realmへの新規ユーザー登録)は、上記いずれの経路でもなく、Keycloakの管理コンソール(`http://localhost:8082`、`admin`/`admin`)から別途行う
admin/go・admin/railsが作成できるのはローカル認証ユーザーのみで、Keycloak側のユーザーは作成できない

</details>

## アーキテクチャ詳細

インタラクティブなアーキテクチャ図: [`docs/1_architecture/bff-gin-system.architecture.html`](docs/1_architecture/bff-gin-system.architecture.html)(evidence付き、検索・フォーカス・ガイド付きビュー対応)

<details>
<summary>テキスト版の詳細(クリックで展開)</summary>

### 全体構成(詳細版、docs/配下の図の要約)

冒頭の概要図をもう少し具体化したもの
正確な図(C4/ER/シーケンス)は`docs/`配下のPlantUML+PNGを参照

```
┌────────────────────────────────────────────────────────────────────────┐
│ ブラウザ(React SPA, :5173)                                            │
│ HttpOnly Cookie(session_id)+X-CSRF-Tokenのみ保持 JWTは一切渡らない │
└──────────────────────────────┬───────────────────────────────────────┘
                                │ ログイン(3方式のいずれか、/loginページから)
                                │  /login       → POST /api/auth/login       (ローカルHMAC・既定)
                                │  /login/rsa   → POST /api/auth/login/rsa   (ローカルRSA)
                                │  「Keycloakでログイン」ボタン → GET /api/auth/login/keycloak
                                ▼
┌────────────────────────────────────────────────────────────────────────┐
│ bff (Go/Gin, :8080)                                                     │
│                                                                          │
│  ローカルHMAC/RSAログイン                    Keycloakログイン            │
│  POST /internal/v1/auth/               Authorization Code + PKCE(S256)  │
│  verify-local-password(backendへ)  ───────────────► Keycloak(:8082)      │
│  検証成功後、bffが自分でJWTを発行                                        │
│  (HS256共有シークレット、またはbff自前のRS256鍵)                         │
│  RS256用に GET /.well-known/jwks.json でbffが自分の公開鍵を配布          │
│  (Keycloakログインの実エンドポイント:                                    │
│   ① GET  http://localhost:8082/realms/training/protocol/openid-connect/auth │
│      (ブラウザがここへリダイレクトされ、Keycloakのログイン画面を操作する)│
│   ② GET  /api/auth/callback?code=...(Keycloakからブラウザ経由でbffへ戻る)│
│   ③ POST http://localhost:8082/realms/training/protocol/openid-connect/token │
│      (bffがcodeをAccess/Refresh/ID Tokenへサーバー間で交換、③はブラウザを経由しない)│
│                                                                          │
│  ログイン成功 → Redisへセッション保存(auth_mode含む)→ session_id Cookie  │
│  以降の各APIリクエスト: session_idからRedisのAccess Tokenを引き、        │
│  Authorizationヘッダに付けてbackendへ転送                                │
│  (backend.task-protocolフラグでTask系はREST v1/gRPC v2のどちらかへ)      │
└───────────────┬──────────────────────────────────┬─────────────────────┘
                │ REST v1(N+1あり、旧実装)   gRPC v2(Preload最適化、新実装)│
                ▼                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ backend(Go、private network限定 REST::8090 / gRPC::9090) │
│                                                                          │
│  authjwt.Dispatcher: JWTの`iss`クレームで検証方式を3方式に振り分け        │
│    iss=Keycloak発行        → KeycloakのJWKS(:8082)で検証                │
│    iss=bff-gin-local-hmac  → 共有シークレット(HS256)で検証              │
│    iss=bff-gin-local-rsa   → bffのJWKS(:8080/.well-known/jwks.json)で検証│
│                                                                          │
│  Task/Label CRUD、Feature Flag評価(MySQLの`feature_flags`を参照)         │
│  Admin::Usersのユーザー管理API(/internal/v1/admin/users、admin/*用)     │
│  GET /internal/v1/feature-flags/export でフラグ一覧をJSON公開           │
│ (bffがHTTP retrieverで定期ポーリングし、自分のFeature Flag評価に使う │
│ `X-Feature-Flag-Poll-Token`ヘッダで認可 admin/go・admin/railsが │
│   MySQLを直接更新→この経路でbffへ伝わるまでにポーリング間隔分の遅延あり) │
└───────────────────────────────┬────────────────────────────────────────┘
                                 ▼
┌────────────────────────────────────────────────────────────────────────┐
│ MySQL 8.0(:13306)                                                       │
│  users / user_passwords(ローカル認証) / user_keycloaks(Keycloak連携)    │
│  webauthn_credentials(パスキー) / tasks / labels / task_labels          │
│  feature_flags / feature_flag_audit_logs                               │
└────────────────────────────────────────────────────────────────────────┘

Redis(:16379、bffだけが利用 backendは一切触れない):
  session:{sessionID}      … ログイン中セッション(Access/Refresh/ID Token, auth_mode)
  lock:session:{sessionID} … トークンリフレッシュの分散ロック
  oidc_pending:{state}     … Keycloakログイン処理中だけの一時状態(PKCE code_verifier等)

--- 外部からのAPI利用(上記ブラウザ経路とは別系統、bffを経由しない) ---

┌───────────────┐  Client Credentials Grant   ┌───────────┐
│ 外部クライアント │ ──────────────────────────► │ Keycloak  │
│(サーバー間連携) │ ◄── access_token(aud=backend)└───────────┘
└───────┬───────┘
        │ (① 事前にトークンエンドポイントを叩いてaccess_tokenを取得する:
        │    POST http://localhost:8082/realms/training/protocol/openid-connect/token
        │    body: grant_type=client_credentials, client_id=external-api-client, client_secret=<秘密>
        │  ② 取得したaccess_tokenを Authorization: Bearer <access_token> として③のAPIへ付与する)
        │ GET /external/v1/tasks (gateway経由:8081、backend自身は:8097)
        ▼
┌────────────────────────────────────────────────────────────────────────┐
│ backend 外部公開API(内部アドレス:8097 公開ポート:8081はgatewayが占有) │
│  RequireExternalClientAuth(azpクレームで検証)                           │
│  backend.external-tasks-pagination-v2 フラグで offset/cursor ページング切替│
└────────────────────────────────────────────────────────────────────────┘

--- 管理系(Basic Auth、ブラウザから別途アクセス) ---

┌──────────────┐              ┌──────────────┐
│ admin/go     │              │ admin/rails  │
│ (:8091)      │              │ (:8092)      │
└──────┬───────┘              └──────┬───────┘
       │ Feature Flag管理: MySQLへ直接GORM/ActiveRecordで接続
       │ ユーザー管理: backendの /internal/v1/admin/users をHTTP経由
       │ (共有シークレット X-Admin-Internal-Token)
       └──────────────┬───────────────────────────┘
                       ▼
              (上記MySQL・backendへ)

--- frontend-rails/without-bff(追加構成、bffを経由しない比較実装、CONTRACT.mdセクション21・22.9) ---

┌────────────────────────────────────────────────────────────────────────┐
│ ブラウザ                                                                 │
└──────────────────────────────┬───────────────────────────────────────┘
                                │ http://localhost:5174/login
                                │  「Keycloakでログイン」→ Authorization Code(Rails自身がKeycloakと直接やりとり)
                                │  「パスキーでログイン」→ POST /auth/passkey/login/begin,finish(同じ画面)
                                ▼
┌────────────────────────────────────────────────────────────────────────┐
│ frontend-rails/without-bff (Rails, :5174)                              │
│  RailsのCookieセッションのみで完結(bff・Redisを経由しない)                │
│  Keycloakログイン成功時: backendへJITプロビジョニング(POST                │
│  /internal/v1/users/provision、自分のaccess_token(aud=backend)を使用)    │
│  パスキー登録(要ログイン、http://localhost:5174/account)・ログインは     │
│  backendの /internal/v1/auth/webauthn/credentials/* を直接呼ぶ           │
│  (bff(Go)・bff-railsと同じ内部APIを共有、credential自体も共有される)     │
└──────────────────────────────┬───────────────────────────────────────┘
                                ▼
                       (上記backendへ、Task一覧等のデータ取得は行わない)
```

### Keycloak(OIDC IdP)

- realm: `training`
  `bff/keycloak/realm-export.json`を`--import-realm`でコンテナ起動時に一度だけ読み込む(永続ボリューム無し、コンテナを作り直すたびにこのファイルの内容で作り直される)
- クライアントは4つ
  - `bff-gin`: confidential、PKCE(S256)必須、Authorization Codeフローのみ有効(`directAccessGrantsEnabled: false`)
    redirect_uriは`http://localhost:8080/api/auth/callback`
  - `external-api-client`: confidential、Service Accounts Enabled(Client Credentials Grant用)
    BFF非経由の外部公開APIの認証専用
  - `frontend-rails`: confidential
    frontend-rails/without-bffが直接Authorization Codeフローを行うための追加構成用クライアント
  - `bff-rails`: confidential
    bff-rails(+frontend-rails/with-bff)向けの追加構成用クライアント
- **Audience Protocol Mapper**: Keycloakの既定動作では、認可コードフローで発行されるAccess Tokenに`aud`クレームが一切含まれない(Client Credentials Grantでは`aud=account`が付くが、認可コードフローでは付かない)
  backendのJWT検証は`aud`の一致を必須にしているため、`bff-gin`・`external-api-client`・`frontend-rails`・`bff-rails`の4クライアント全てに固定値`backend`を`aud`として注入する`oidc-audience-mapper`(`included.custom.audience=backend`)を設定している
- `KC_HOSTNAME=localhost`・`KC_HOSTNAME_PORT=8082`により、Keycloakが発行するissuer/各種endpoint URLは常に`http://localhost:8082/...`になる
  - backend/bffはコンテナ化せずネイティブ実行しているため、ブラウザが使うURLとbackend/bffが使うURLが完全に一致し、issuer検証で破綻しない(詳細は上の「ローカル開発でのプロセス配置」参照)
- テストユーザー: `general-user`/`password`(role: general)、`admin-user`/`password`(role: management)
- ログアウトはRP-Initiated Logoutのみ実装(Keycloak管理コンソールからの強制ログアウトに追従するBackchannel Logoutは対象外)

### ローカル(非Keycloak)認証(CONTRACT.mdセクション16)

Keycloak(OIDC)一本に加えて、比較用に「bff自身がユーザー名/パスワードを検証しJWTを発行する」認証方式を追加している
Feature Flagでの切り替えも検討したが、ログイン前はセッションが無くuser_id単位のtargetingができないため不採用とし、
**ログインURLを分ける** 方式にした

| URL(frontend) | 送信先(bff) | 方式 | JWTの署名 |
|---|---|---|---|
| `/login`(既定) | `POST /api/auth/login` | ローカル | HS256(共有シークレット) |
| `/login/rsa` | `POST /api/auth/login/rsa` | ローカル | RS256(bffが起動時に生成する自分の鍵ペア `GET /.well-known/jwks.json`でJWKSを公開) |
| `/login/keycloak` | `GET /api/auth/login/keycloak` | Keycloak OIDC(既存) | RS256(Keycloakの鍵、既存のJWKS検証) |

- **スキーマ**: `users`テーブルからは認証情報を分離済み(`keycloak_sub`カラムは廃止)
  `user_passwords`(`password_digest`・`password_expires_at`)と`user_keycloaks`(`keycloak_sub`)の2テーブルに分かれている
- **seedされるローカルユーザー**: `local-user@example.com` / パスワード `password`、`password_expires_at`は`2099-12-31`
  登録画面・パスワード変更画面は用意していない(実務でも最初の1人は管理者が手動発行するのが一般的、という割り切り)
- **パスワード期限切れの挙動**: 期限切れの資格情報で`/login`(または`/login/rsa`)にログインしようとすると401 `{"error":"password_expired"}`が返り、そもそもログインできない(セッションを作らない、「ログイン中に期限が来て強制ログアウトされる」機能ではない)
- **パスワード照合はbackend経由**: bffはDBに直接アクセスしない既存方針を維持するため、`POST /internal/v1/auth/verify-local-password`(共有シークレット`X-Local-Auth-Internal-Token`で認可)をbackendに新設し、bffはここへ問い合わせてからJWTを発行する
- **RSA鍵は非永続化**: bffが起動時に一度だけ`rsa.GenerateKey`で生成し、プロセスメモリ上にのみ保持する
  bff再起動でRSA版のセッションは検証不能になり再ログインが必要(本番であれば鍵のローテーション・永続化が必要になる、学習用の簡略化)
- **backend側のJWT検証**: `iss`クレームで3方式(Keycloak / `bff-gin-local-hmac` / `bff-gin-local-rsa`)を振り分ける`authjwt.Dispatcher`が、`iss`を見てから対応するverifier(HMAC共有シークレット、またはbffのJWKS)で実際の署名検証を行う2段階構造
- ローカルセッションには`refresh_token`が無い(有効期限24時間、更新なし)
  backendから401が返ってもリフレッシュは試みずセッションを破棄する(Redisの`Session.auth_mode`で分岐)
  ログアウトも`auth_mode`が`keycloak`の場合のみKeycloakのRP-Initiated Logoutを行う
- 新規環境変数:
  - backend: `LOCAL_AUTH_INTERNAL_TOKEN`(既定`local-dev-local-auth-internal-token`)、`LOCAL_AUTH_HMAC_SECRET`(既定`local-dev-hmac-shared-secret-change-me`)、`LOCAL_AUTH_RSA_JWKS_URL`(既定`http://localhost:8080/.well-known/jwks.json`、bffを指す)
  - bff: `LOCAL_AUTH_VERIFY_PASSWORD_URL`(既定`{BACKEND_REST_BASE_URL}/internal/v1/auth/verify-local-password`)、`LOCAL_AUTH_INTERNAL_TOKEN`・`LOCAL_AUTH_HMAC_SECRET`(いずれもbackendと同じ値にすること)

#### HMAC版とRSA版の違い

「誰が鍵を持っているか」という点に集約される

| | HMAC版(`/login`、既定) | RSA版(`/login/rsa`) |
|---|---|---|
| 署名アルゴリズム | HS256(共通鍵暗号) | RS256(公開鍵暗号) |
| 鍵の数 | 1つ(bffとbackendが同じ秘密の値を共有) | 2つ(bffだけが持つ秘密鍵+誰でも見られる公開鍵) |
| 鍵の実体 | 環境変数`LOCAL_AUTH_HMAC_SECRET`(固定の文字列、bff/backend両方に同じ値を設定) | bffが起動時に`rsa.GenerateKey`でその場で生成するペア |

- **HMAC版**: bffがログイン時に共有シークレットで署名し、backendも同じ値で検証する(対称鍵)
  合言葉が漏れると、漏れた側(backend含む)が本物そっくりの偽トークンを作れてしまう弱点がある
- **RSA版**: bffが起動時に自分だけの秘密鍵・公開鍵ペアを作り、署名には秘密鍵を使う
  backendは`GET /.well-known/jwks.json`で公開鍵を取得して検証するだけで、署名する能力自体は持たない(非対称鍵)
  これはKeycloakが実際にやっていること(公開鍵をJWKSで配布し、秘密鍵は自分だけが持つ)と同じ仕組みで、bffが「小さな自前IdP」として振る舞っている
- 機能的にはRSA版だけあれば十分だが、「対称鍵(シンプルだが鍵共有が必要)」と「非対称鍵(bff自身がミニIdPとして振る舞う、実務でよく使われる形)」を比較するために両方用意している
  パスワード照合ロジック・対象ユーザー・セッション有効期限は両方式で共通で、違うのは発行されたJWTの署名方式だけ

### Redis

Redisを使うのはbffだけで、backendは一切Redisに触れない(backendはステートレスにJWT検証だけを行う設計のため)

| キー | 内容 | TTL |
|---|---|---|
| `session:{sessionID}` | `{user_id, auth_mode, access_token, refresh_token, id_token, access_token_exp, refresh_token_exp}` | refresh_tokenの有効期限に追従(ローカル認証はaccess_token_expと同値) リフレッシュ成功の都度更新 |
| `lock:session:{sessionID}` | 分散ロック用マーカー(値は任意) | 数秒(SETNXで取得、リフレッシュ完了後にDEL) |
| `oidc_pending:{state}` | ログイン開始時に生成した`{code_verifier, redirect}`(PKCE検証用+元の遷移先) | 5分(`GetDel`でCallback到達時に1回だけ読み出し即削除) |

- **セッションストア**: ブラウザにはセッションIDだけがHttpOnly Cookieとして渡り、トークン本体はブラウザに一切渡さない(BFFパターンの核)
- **分散ロック**: BFFは複数インスタンスでの稼働を想定したステートレス設計のため、同一セッションに対する同時アクセスがそれぞれ独立にトークンリフレッシュを試みると、Keycloakのrefresh token rotationにより一方が強制失効しうる
  SETNXによる排他制御でこれを防ぐ
- **ログイン処理中の一時状態**: Callback到達時に`GetDel`で1回読み出すと同時に消える(再利用防止)

具体的な確認コマンドは[確認手順「Redisの中身」](#redisの中身)を参照

### Feature Flag(frontend・bff・backendそれぞれの責務)

インタラクティブなデータフロー図: [`docs/3_feature-flag-dataflow/bff-gin-feature-flag-propagation.dataflow.html`](docs/3_feature-flag-dataflow/bff-gin-feature-flag-propagation.dataflow.html)(admin/go・admin/rails→MySQL→backend/bffのポーリング→Browserまでの伝播経路と、どのフラグがどこまで届くかの境界を図示)

このプロジェクトには **複数の独立したFeature Flag評価ポイント** があり、「どこで評価するか」自体が設計上のポイント
MySQLの`feature_flags`テーブルが正本で、admin/go・admin/rails(管理画面)から編集する

| フラグ名 | 評価する場所 | Reactへの公開 | 用途 |
|---|---|---|---|
| `frontend.tasks-ts-rewrite` | bff | `/api/me`の`feature_flags`経由で値だけ渡す | Task一覧の新(TS)/旧(JS)コンポーネント切り替え |
| `frontend.task-create-ux` | bff | 同上 | Task登録UXのinline/modal/page切り替え(3値) |
| `bff.tasks-backend-v2` | bff | 渡さない(BFF内部限定) | 非推奨 `backend.task-protocol`に統合済み(履歴保持のため画面には残置) |
| `backend.task-protocol` | backend | 該当なし | bffがbackendへのリクエストをREST/gRPCどちらで送るか |
| `backend.task-language` | backend | 該当なし | Task CRUDの実装言語(go/rust/scala-http4s/scala-pekko/rails)切り替え |
| `backend.external-tasks-pagination-v2` | backend | 該当なし(外部APIクライアント向け) | 外部公開APIのoffset/cursorページング切り替え |
| `backend.external-tasks-orm` | backend | 該当なし | 外部公開APIのTask一覧取得のORM実装(gorm/bob)切り替え |
| `frontend-rails.oidc-gem` | frontend-rails/without-bff | 該当なし | 使用するOIDC gem(omniauth-openid-connect/openid_connect)切り替え |

- **frontend自身はFeature Flag providerを一切持たない**
  `useFeatureFlag`フックはbffが評価済みの値(`/api/me`のレスポンス)を読むだけで、フロントエンドから直接flag providerへ問い合わせることはしない
  これは「BFFが外部サービスへの資格情報をブラウザに露出させない」という設計方針(CONTRACT.mdセクション4)をFeature Flagにも一貫して適用したもの
- **bffが評価するフラグ**は同じMySQLテーブル・同じOpenFeature Go SDK + GO Feature Flag(HTTP retriever)のインスタンスを使うが、公開範囲が異なる
  React側の切り替えに使うものは`/api/me`で公開し、BFF内部のルーティングだけに使うものはReact側には一切見せない
- **backendは自分専用のFeature Flag評価インスタンスを別途持つ**
  理由は、外部公開API(`/external/v1/tasks`)がBFFを経由しない唯一の例外的な経路であり、この経路にはbffのFeature Flag評価が介在しようがないため
  backendは同じMySQLテーブルを自分で直接ポーリングする(bffのようにHTTP経由にはしない、backend自身が元々MySQL接続を持つため)
- targeting keyの使い分け: ログイン中ユーザーに関わるフラグはユーザーの`user_id`、外部公開API関連のフラグはAPIクライアントの`client_id`(人間のユーザーが存在しない機械間通信のため)

admin画面での変更が実際にどちらの実装へ反映されたかを確認する具体的な手順・コマンドは[確認手順「Feature Flagの切り替え」](#feature-flagの切り替え実際に挙動が変わることの確認)を参照

### Strangler Fig(ストラングラー・フィグ)パターン

「巨大な木に絡みつき、内部から古い部分を少しずつ置き換えながら成長する」ことに由来する、システムを一度に全面刷新せず新旧を並存させながら段階的に置き換えていく移行手法
このプロジェクトには意図的に、**2種類・異なるレイヤーのStrangler Fig 用デモ**を用意している

**1. フロントエンド(コンポーネント単位)** — `frontend.tasks-ts-rewrite`

- 旧実装: `frontend/src/features/tasks/legacy/TaskList.jsx`(JavaScript)
- 新実装: `frontend/src/features/tasks/TaskList.tsx`(TypeScript)
- `TaskListSwitch.tsx`がFlagで新旧のどちらを描画するか切り替える
- **重要な設計原則**: 旧実装は現役の本番コードという体で作るべきであり、新実装より機能が劣っていてはならない
  旧実装にも登録・編集・削除のフル機能を実装しており、両実装の違いは最終的に「TypeScriptで書かれているかJavaScriptで書かれているか」だけになっている

**2. BFF(ルーティング単位)** — `backend.task-protocol`

- 旧実装: backendのREST v1(`/internal/v1/tasks`、labelsをタスクごとに個別クエリ取得する意図的なN+1あり)
- 新実装: backendのgRPC v2(`TaskService`、`Preload`でN+1解消・cursorページング採用)
- bffの`proxy/task_route.go`がFlagでどちらへリクエストを転送するか切り替え、レスポンス形状の違い(REST JSON⇔protobuf、offsetページング⇔cursorページング)もbffが吸収してReactには常に同じ形で返す
- 「BFFが窓口となり、背後の新旧実装を切り替える」設計は、社内の実例(BFFが窓口として、まだ移行していない機能をレガシーCore APIに委譲する構成)から着想を得ている
  実際の現場では「レガシー全体 vs 新backend全体」のような粗い単位で切り替えることが多いが、
  ここでは比較のため「同じTask機能の新旧実装」という細かい単位にしている

**両方とも「Release Toggle」であり、恒久的に残すフラグではない**
新実装のロールアウトが完了し十分な期間問題が無いことを確認したら、フラグ定義・旧実装・切替コンポーネントをまとめて削除する運用が前提

なお、外部公開APIの`backend.external-tasks-pagination-v2`・`backend.external-tasks-orm`は同じFeature Flagの仕組みを使っているが、「レガシーを置き換える」という文脈ではなく「複数の実装方式を比較する」ための実装であり、Strangler Fig(移行のための一時的な切り替え)には分類していない

</details>

## 前提バージョン(Mac M2〜M5)

| 項目 | バージョン |
|---|---|
| macOS | Sonoma以降(Apple Silicon M2〜M5) |
| Docker Desktop | 最新安定版(Docker Engine 29系 / Docker Compose v5系) |
| Go | 1.27系(ローカルでの`go test`実行に使用 コンテナビルドはDockerfile内で完結) |
| Node.js | 22 LTS以降(frontend/e2eで使用) |
| pnpm または npm | frontendの依存管理に使用 |
| PlantUML | 図の再生成が必要な場合のみ(`brew install plantuml`) |

Apple Siliconのため、`docker pull`されるイメージはarm64対応のものを使用すること(mysql:8.0, redis, keycloakの公式イメージはいずれもマルチアーキ対応)

## セットアップ手順

### 1. Docker Desktopを起動する

Launchpadなどから起動し、メニューバーのDockerアイコンが起動完了状態になるまで待つ

```sh
docker --version         # Docker version 29.x系であること
docker compose version   # v5.x系であること
```

### 2. コンテナ群を起動する

```sh
cd frontend_passkey-go_bff-backend-multi
docker compose up -d --wait mysql redis keycloak swagger-ui

# 起動確認
docker compose ps
docker compose logs keycloak | tail -30   # "Imported realm training" 等を確認
```

- `mysql`: パスワードなしのrootのみ(開発用のテストのため)
- `redis`: セッションストア
- `keycloak`: `--import-realm`により`bff/keycloak/realm-export.json`のrealm(`training`)・クライアント・テストユーザーが自動投入される
- `swagger-ui`: 外部公開API(`backend/openapi/external-api.yaml`)を閲覧・Try it outできる(`http://localhost:18080`)

停止・再起動・作り直しのコマンド:

```sh
# コンテナを止めるだけ(データ・コンテナ自体は残る)
docker compose stop

# 再起動(healthyになるまで待つ、前回と同じ状態に戻る)
docker compose up -d --wait mysql redis keycloak swagger-ui

# コンテナごと作り直す(bff-gin-mysql-dataボリュームは残るのでMySQLのデータは消えない)
docker compose down
docker compose up -d --wait mysql redis keycloak swagger-ui

# MySQLのデータも含めて全部作り直す(データが消えるので注意)
docker compose down -v
docker compose up -d --wait mysql redis keycloak swagger-ui
```

`bff/keycloak/realm-export.json`を変更した場合は、Keycloakサービスには永続ボリュームが無いため、以下だけで変更が反映される(MySQLのデータは保持される):

```sh
docker compose stop keycloak
docker compose rm -f keycloak
docker compose up -d --wait keycloak
```

(`docker compose restart keycloak`ではrealmの再読み込みが起きないため、必ず`rm`または`down`でコンテナ自体を作り直すこと)

### 3. MySQLへログインして疎通確認する

```sh
docker compose exec mysql mysql -uroot -e "SHOW DATABASES;"
```

### 4. マイグレーションを実行する

```sh
cd backend
go run ./cmd/migrate up   # seedも含まれる
```

- `DB_DSN`は未設定でも既定値(`root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true`)が使われる
  別のDB接続先を使う場合のみ明示的に上書きする
- ロールバック: `go run ./cmd/migrate down`(最新の1ステップを巻き戻す)、再適用は`up`を再実行

### 5. backendを起動する

```sh
cd backend
go run ./cmd/server
# REST v1: http://localhost:8090 / gRPC v2: localhost:9090 / 外部公開API(内部アドレス): http://localhost:8097
```

`DB_DSN`/`KEYCLOAK_ISSUER`/`EXPECTED_AUDIENCE`等はdocker-compose.yamlの構成に対応する既定値が入っているため、通常は環境変数の指定なしでそのまま起動できる
詳細なログ(GORMが発行したSQLを含む)を見たい場合は`LOG_LEVEL=debug go run ./cmd/server`

起動に失敗する場合は、マイグレーション未適用(手順4を先に実行する)か、対象ポート(8090/9090/8097)が別プロセスに使われていないかを確認する(`lsof -iTCP:<port> -sTCP:LISTEN -P`)

#### 5.1 多言語backend(Rust/Scala×2/Rails)を起動する(追加構成)

Task CRUD(内部REST/gRPC)をGo以外の4言語でも実装している
`backend.task-language`フラグで切り替える際に必要

```sh
# Rust(REST:8093 gRPC:9093 外部:8098)
cd backend-rust && cargo run

# Scala(http4s)(REST:8094 gRPC:9094 外部:8099)
cd backend-scala-http4s && sbt run

# Scala(Pekko)(REST:8095 gRPC:9095 外部:8100)
cd backend-scala-pekko && sbt run

# Rails(REST:8096 / 外部:8101 / gRPC:9096、3プロセス構成)
cd backend-rails
bundle install   # 初回のみ
bundle exec puma -C config/puma.rb            # REST :8096
bundle exec bin/grpc_server                     # gRPC :9096(別プロセス)
bundle exec puma -C config/puma_external.rb    # 外部公開API :8101(こちらも別プロセス)
```

Rust/Scala×2/Railsはいずれも独自のマイグレーションを持たない
スキーマの正本は`backend/migrations`のみで、他4言語は同じMySQL(`bff_gin_development`)を読み書きするだけ
Railsだけ`GRPC::RpcServer`のブロッキングイベントループがPumaと同居できないため3プロセス構成になる

### 6. bffを起動する

```sh
cd bff
go run ./cmd/server
# http://localhost:8080
```

`OIDC_ISSUER_URL`/`OIDC_CLIENT_ID`/`OIDC_CLIENT_SECRET`/`OIDC_REDIRECT_URL`/`REDIS_ADDR`/`BACKEND_REST_BASE_URL`/`BACKEND_GRPC_ADDR`/`FRONTEND_BASE_URL`はいずれも既定値のまま起動できる
コード変更後は起動中のプロセスを再起動すること(`go run`は起動時にコンパイルするため、再起動を忘れると古いコードのまま動き続ける)

### 7. frontendを起動する

```sh
cd frontend
npm install   # 初回のみ
npm run dev
# http://localhost:5173
```

### 8. admin画面を起動する

```sh
# Rails版(:8092)
cd admin/rails
bundle install   # 初回のみ
bin/rails server -p 8092

# Go+Gin版(:8091)
cd admin/go
go run ./cmd/server
```

どちらも同じMySQLの同じテーブル(`feature_flags`/`feature_flag_audit_logs`)を直接操作する比較実装で、認証はHTTP Basic Auth(既定`admin`/`password`、環境変数`ADMIN_BASIC_AUTH_USER`/`ADMIN_BASIC_AUTH_PASSWORD`で変更可)
テーブル自体はbackendのマイグレーションが正本なので、起動前に手順4を済ませておくこと

#### 8.1 外部公開APIゲートウェイを起動する(追加構成)

`backend.task-language`の切り替え結果に応じて外部公開APIを振り分ける
Go製・nginx製のいずれかを起動する(どちらも:8081を使うため同時起動不可)

```sh
# Go製
cd gateway/go
go run .

# nginx製
cd gateway/nginx
./start.sh
```

#### 8.2 frontend-rails(without-bff / with-bff)+ bff-railsを起動する(追加構成)

```sh
# frontend-rails/without-bff(:5174、bffを使わずRails自身がKeycloakと直接OIDCを行う)
cd frontend-rails/without-bff
bundle install   # 初回のみ
bin/rails server -p 5174

# bff-rails(:8102)+ frontend-rails/with-bff(:5175、React+bffと同じ役割分担をRailsで再現)
cd bff-rails
bundle exec puma -C config/puma.rb
cd frontend-rails/with-bff
bin/rails server -p 5175
```

### 9. まとめて再起動する場合

```sh
# 1. 起動中のbackend/bff/frontend等を一度全部止める(Ctrl+C)
# 2. docker composeは維持でOK 念のため状態確認
docker compose ps
# 3. マイグレーションが最新か確認・適用
cd backend && go run ./cmd/migrate up
# 4. 各プロセスを起動(すべて環境変数なしで動く)
cd backend && go run ./cmd/server
cd bff && go run ./cmd/server
cd frontend && npm run dev
```

## 確認手順

環境構築後、一通りの機能が壊れていないかを確認するための手順集
自動テストと、手動での動作確認に分かれる

### 自動テストの実行

#### Frontend単体テスト

```sh
cd frontend
tsc --noEmit        # 型チェックのみ(ビルドはしない)
npm run test        # Vitest(本体)
npm run test:jest   # Jest(比較用、Vitestと同じ網羅性を目指し全ファイルをミラー)
npm run test:watch  # ファイル変更を監視して再実行
```

#### Go単体テスト

```sh
cd backend && go test ./...
cd bff && go test ./...
cd admin/go && go test ./...
```

#### Go結合テスト(実MySQL/Redis/Keycloak使用)

```sh
docker compose up -d --wait mysql redis keycloak swagger-ui
cd backend && go test -tags=integration ./...
# admin/goもDBが必要なテスト(TEST_DB_DSN未設定ならスキップ)を含む
cd admin/go && TEST_DB_DSN="root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true" go test ./...
```

#### Rails単体テスト(admin/rails)

```sh
cd admin/rails
bundle install   # 初回のみ
bundle exec rspec
```

モデルスペック・リクエストスペックに加え、Capybara(`rack_test`ドライバ)によるsystem specを1本含む(一覧表示→編集→保存→変更履歴確認、という一連の画面操作を通しで検証する簡易的なE2E)

#### Lint

```sh
golangci-lint run ./...
```

backend/bff/admin-goそれぞれのディレクトリで実行、または各`.golangci.yml`を指定(現時点で`.golangci.yml`があるのはbackendのみ)

#### E2E(6種、JS3種 + Go3種)

CONTRACT.mdセクション9・18に基づき、同一のシナリオを6フレームワークで実装している
**複数のスイートを同時実行すると、実行中に一時的に切り替えるFeature Flag(task-create-ux等)が競合することがあるため、必ず1つずつ順番に(直列で)実行すること**

```sh
# Playwright
cd e2e/playwright && npm install && npx playwright install && npm test

# Cypress
cd e2e/cypress && npm install && npx cypress run

# Selenium
cd e2e/selenium && npm install && npm test

# chromedp(Go)
cd e2e/chromedp && go test ./... -v

# go-rod(Go)
cd e2e/go-rod && go test ./... -v

# playwright-go(Go)
cd e2e/playwright-go && go test ./... -v
```

実行前に`docker compose up -d --wait`で全サービスを起動し、backend/bff/frontend/admin/go(Playwrightのadmin.spec.ts等が使用)も起動しておくこと

いずれも「ログイン→タスク一覧表示→作成→更新→削除→ログアウト」を基本導線とし、Task登録UXのmodal/page版シナリオ、Feature Flag ON/OFF切り替え、`backend.task-language`・`backend.external-tasks-orm`切り替え(task-languageはbackend-rust未起動時に自動スキップ)、resilience系(ネットワーク遅延・戻る/リロード・複数タブ・パスキー登録後のパスワード認証後方互換)、security系(Cookie改ざん・CSRFヘッダ欠落・XSS・ログアウト後の情報露出)の各シナリオを含む
Playwright/Selenium/chromedp/go-rod/playwright-goの5種には仮想認証器を使ったパスキー登録→ログインのシナリオも含まれる(Cypressのみ、WebAuthn仮想認証器の第一級APIが無いため見送り)
frontend-rails/without-bff独自のパスキー機能(CONTRACT.mdセクション22.9)のシナリオは6種全てに含まれる
Cypressのみ複数タブシナリオを見送っている(仕様上の制約、CONTRACT.mdセクション18.5参照)

既知の制約:

- `backend.task-language`・`backend.external-tasks-orm`の切り替えシナリオは、playwright-goには未実装(`backend_task_language_test.go`・`backend_external_tasks_orm_test.go`が存在しない
  他5種には実装済み)
- Go製3フレームワーク(chromedp/go-rod/playwright-go)は、task-create-uxのFeature Flagを`t.Cleanup`でinlineへ戻す処理に反映待ちが入っておらず、直後に実行される別テスト(`TestTaskCRUD`)がflag未反映のまま失敗・ハングすることがある(`TestTaskCRUD`単独実行なら即pass
  実行順序に依存する)
- frontend-rails/without-bffでKeycloakログイン→パスキー登録の一連の流れで、セッションCookieが4KB上限を超え`ActionDispatch::Cookies::CookieOverflow`が発生し途中の画面へ遷移できなくなることがある(Playwright/Selenium/chromedp/go-rod/playwright-goで再現)
- Seleniumの`backend-task-language.test.js`が再現性を持って失敗することがある(タスク更新後の一覧反映待ちタイムアウト、原因未特定)

### 手動での動作確認

#### ログイン(3方式)

インタラクティブなシーケンス図: [`docs/2_sequence-diagrams/bff-gin-login-flows.sequence.html`](docs/2_sequence-diagrams/bff-gin-login-flows.sequence.html)(local-hmac/local-rsa/keycloak-login/logoutの4 guided views)
インタラクティブなライフサイクル図(セッションの状態遷移): [`docs/4_lifecycle-diagrams/bff-gin-session-lifecycle.lifecycle.html`](docs/4_lifecycle-diagrams/bff-gin-session-lifecycle.lifecycle.html)

ローカルHMAC・ローカルRSA・Keycloak、それぞれログイン〜ログアウトまで

- `/login`(ローカルHMAC)で`local-user@example.com` / `password`でログインできる → タスク一覧画面へ遷移する
- ログアウトできる(HMACセッション)
  ログアウト後、同じCookieのままAPIを叩くと401になることも確認する
- `/login/rsa`(ローカルRSA)で同じユーザーでログインでき、ログアウトもできる
- `/login`の「Keycloakでログイン」ボタンから`general-user`でログインできる → Keycloakのログイン画面が一度表示され、成功後タスク一覧へ戻る
- ログアウトできる(Keycloakセッション、RP-Initiated Logout)
  ログアウト後にKeycloak側のログイン画面が表示されれば、Keycloak側のセッションも正しく切れている
- `admin-user`(role: management)でもKeycloakログインできる
- 誤ったパスワードで`/login`を試すと「メールアドレスまたはパスワードが正しくありません」が表示されログインできない
- `[SECURITY]` `/login?redirect=http://evil.example.com/`のような外部URLを指定しても、ログイン後に外部サイトへ遷移しない(Open Redirect対策、相対パス以外はfrontendのトップ等へフォールバックする)

#### Task機能のCRUD

インタラクティブなシーケンス図(bff→backendの振り分け): [`docs/2_sequence-diagrams/bff-gin-task-create.sequence.html`](docs/2_sequence-diagrams/bff-gin-task-create.sequence.html)(REST(v1)/gRPC(v2)の2 guided views)
インタラクティブなライフサイクル図(ステータス遷移): [`docs/4_lifecycle-diagrams/bff-gin-task-lifecycle.lifecycle.html`](docs/4_lifecycle-diagrams/bff-gin-task-lifecycle.lifecycle.html)

作成・一覧・編集・削除、新旧実装の両方(inline版)

- タスクを新規作成できる(名前・ステータス・期限・ラベル) → 「タスクを作成しました」
- 作成したタスクが一覧に表示され、編集(更新)・削除ができる
- タスク名を21文字以上入力すると、フォームまたはサーバー側で弾かれる
- タスクが0件のとき、一覧が崩れず表示される
- `[SECURITY]` タスク名に`<img src=x onerror=alert(1)>`のような文字列を入力しても、実行されずそのまま文字列として表示される(XSS対策、Reactの自動エスケープに依存)
- `[SECURITY]` 存在しないタスク名(20文字超え等)でわざとエラーを起こすと、エラーメッセージが読める日本語文になっている(生のJSON文字列がそのまま表示されない
  人間が読める文言、例:「タスク名は20文字以内で入力してください」)
- `[SECURITY]` 削除ボタン押下直後(通信中)に別の行の編集・削除を続けて行っても、最終的な一覧が古い応答で上書きされない(素早く連続操作しても最終的に正しい一覧に収束する)

#### Task登録UXの3パターン(inline/modal/page)

多値Feature Flag`frontend.task-create-ux`
TS/JS実装(`tasks-ts-rewrite`)とは独立した軸

- admin画面で`frontend.task-create-ux`を編集すると、既存のON/OFFトグルとは違いinline/modal/pageの3択selectとして表示される
- `modal`に切り替えると、一覧に「タスクを登録」ボタンが表示され、クリックするとモーダルでフォームが開く
  行の「編集」ボタンもモーダルで開き、値が入っている
  モーダルを閉じても一覧の裏側の状態は崩れない
- `[SECURITY]` (modal、キーボードのみで操作) モーダルを開くとフォーカスがモーダル内に移り、Tabキーで背後の一覧要素へは抜けない
  閉じると「タスクを登録」ボタンへフォーカスが戻る(a11y、マウスを使わずTab/Escapeキーだけで一連の操作ができる)
- `page`に切り替えると、一覧に「タスクを登録」リンクが表示され、クリックすると`/tasks/new`へ遷移する
  作成成功で自動的に`/tasks`へ戻る
  フォーム画面(`/tasks/new`・`/tasks/:id/edit`)に「一覧へ戻る」リンクがあり、保存せずに一覧へ戻れる
- `frontend.tasks-ts-rewrite`をONにした状態でも、上記modal/page双方が同様に動く(legacy(JavaScript)実装側にも同じ3パターンが独立して実装されている)
- 確認後、`frontend.task-create-ux`を`inline`に戻す(既定値)

#### ラベル機能のCRUD

`/labels`画面での作成・編集・削除

- ラベルを新規作成・編集・削除できる
- 作成したラベルがタスクのラベル選択(Select2)に表示され、タスクに紐付けられる
- `[SECURITY]` 使用中(いずれかのタスクに紐付いている)のラベルを削除しようとすると422エラーで拒否される
- `[SECURITY]` タスクのラベル選択で同じラベルを重複して送信しても(通常のUI操作では起きないはずだが)、タスク作成/更新が500エラーにならない(直接APIを叩いた場合の防御確認
  Go/Rust/Scala(http4s)/Scala(Pekko)/Railsいずれも重複を排除して処理する)

#### Feature Flagの切り替え(実際に挙動が変わることの確認)

インタラクティブなデータフロー図: [`docs/3_feature-flag-dataflow/bff-gin-feature-flag-propagation.dataflow.html`](docs/3_feature-flag-dataflow/bff-gin-feature-flag-propagation.dataflow.html)

admin画面でフラグを変更 → フロント/BFF/外部APIの挙動が変わることまで確認

```sh
# bffのログ(標準出力)で振り分け結果を確認
# "backend.task-language/backend.task-protocolの評価結果によりTaskの実装を振り分け" の implementation を見る
# 例: "language":"go","protocol":"grpc","implementation":"go:grpc"
```

- `frontend.tasks-ts-rewrite`を admin画面でON→タスク画面が新実装(TypeScript)に切り替わる(ブラウザのコンソールに`[feature-flag] frontend.tasks-ts-rewrite=true → 新実装...`のログが出る)
- `backend.task-protocol`をrest⇄grpcに切り替えると、bffのログに振り分け結果(implementation)が出る
- `backend.external-tasks-pagination-v2`をON→外部公開APIのレスポンス形状が変わる(offsetの`page`/`page_size`→cursorの`next_cursor`)
- `backend.external-tasks-orm`をgorm⇄bobに切り替えても、外部公開APIのレスポンス形状は一切変わらない(内部実装の入れ替えのみ)
- admin/goでの変更が、しばらく待つとbffのポーリング経由で反映される(即時ではない、数秒〜十数秒のラグ)
- `frontend.task-create-ux`(3値)を切り替えると、一覧画面の登録UIがinline/modal/pageに切り替わる

#### Admin画面: Feature Flag管理(Go / Rails比較)

同じMySQLテーブルを2つの独立実装から操作する

- admin/goの一覧画面(:8091)でフラグ一覧(8種)が表示される: `frontend.tasks-ts-rewrite` / `bff.tasks-backend-v2`(非推奨) / `backend.external-tasks-pagination-v2` / `frontend.task-create-ux` / `backend.task-language` / `backend.task-protocol` / `frontend-rails.oidc-gem` / `backend.external-tasks-orm`
- admin/goでフラグのenabled/default_variationを変更でき、変更履歴(audit log)画面に記録される
- admin/rails(:8092)でも同じ8種が表示され、変更・履歴確認ができる
- 片方(例: admin/go)で変更した内容が、もう片方(admin/rails)の画面をリロードすると反映されている(同じMySQLテーブルを直接見ているため)
- Basic Auth無しでアクセスすると401になる(両方)

#### Admin画面: ユーザー管理(Go / Rails比較)

新規作成・role変更・削除・パスキー登録状況
backend経由(直接DBではない)

- admin/goの`/users`一覧に既存ユーザーが表示され、ローカル認証ユーザーを新規作成できる(name/email/password/role) → 一覧に反映され、作成したメールアドレス・パスワードでそのまま`/login`からログインできる
- admin/goでrole変更ができる
- 管理者(role: management)が1人しかいない状態にして削除・降格しようとすると「最後の管理者」エラーメッセージが表示され拒否される
- `[SECURITY]` (上級者向け) 同じユーザーで2つのタブから同時に「最後の管理者」を削除しようとしても、片方しか成功しない(`SELECT ... FOR UPDATE`によるTOCTOU対策
  backend側のregression testで自動確認済み)
- admin/goでユーザーを削除できる(management以外)
  同様にadmin/railsの`/users`でも一覧・作成・role変更・削除・最後の管理者ガードを確認する
- 重複するメールアドレスで作成しようとすると、分かりやすいエラーメッセージが出る(両方)
- ログイン中のユーザーを別ブラウザ/admin画面から削除すると、タスク/ラベル画面へ遷移しようとした際に自動的にログイン画面へ戻る(401を受けてbff側のセッションが破棄される)
- 一覧に「パスキー」列があり、パスキー登録済みユーザーは✅登録済み、未登録ユーザーは未登録と表示される(admin/go・admin/rails両方)
- `[SECURITY]` (上級者向け) パスキー登録済みのユーザーを削除しても、DBに`webauthn_credentials`の孤立行が残らない(ユーザー削除処理は`user_passwords`→`user_keycloaks`→`webauthn_credentials`→`task_labels`→`tasks`の順にカスケード削除する
  確認する場合は`SELECT * FROM webauthn_credentials WHERE user_id = <削除したユーザーのid>;`が0件になることを見る)

#### パスキー(WebAuthn)

インタラクティブなシーケンス図: [`docs/2_sequence-diagrams/bff-gin-passkey-flow.sequence.html`](docs/2_sequence-diagrams/bff-gin-passkey-flow.sequence.html)(登録→ログアウト→パスキーのみログインの3 guided views)

既存ユーザー(ローカル認証)への追加認証手段
discoverable credential方式
このセクションはコア構成(React frontend + bff(Go) + backend)のみが対象
bff-rails・frontend-rails/without-bff独自のパスキー機能は後述の「frontend-rails + bff-rails」を参照

```sql
-- パスキー登録状況の確認・削除
docker compose exec mysql mysql -uroot bff_gin_development -e \
  "SELECT id, user_id, sign_count, transports, name, created_at FROM webauthn_credentials;"
docker compose exec mysql mysql -uroot bff_gin_development -e \
  "DELETE FROM webauthn_credentials WHERE user_id=1;"
```

- `/login`(ローカルHMACまたはRSA)でログインする(パスキーは既存ユーザーへの追加の認証手段のため、まず何らかの方法でログインしておく必要がある
  Keycloak発行ユーザーはこのパスキー機能の対象外)
- アカウント画面(`/account`)で「パスキーを登録」を実行する → ブラウザ標準のWebAuthn UI(Touch ID/Windows Hello等)が表示され、登録成功のメッセージが出る
- ログアウト後、`/login`画面の「パスキーでログイン」ボタンから、メールアドレス入力無しでログインできる(discoverable credential方式)
- admin画面(admin/go・admin/rails)のユーザー一覧で、このユーザーが「パスキー: 登録済み」と表示される
- (オプション、Chromeの場合) DevToolsのWebAuthnタブで仮想認証器を有効にすると、実機の生体認証無しで登録・ログインを試せる(e2eのGo/Playwright実装も同じCDP Virtual Authenticator機能を使って自動化している)

#### セキュリティ回帰確認

Open Redirect・Session Fixation・IDOR・TOCTOU等の脆弱性の手動確認

```sh
# CSRFトークン無しでのPOST(ブラウザのコンソールから、ログイン中の状態で実行)
fetch('/api/tasks', {method:'POST', headers:{'Content-Type':'application/json'}, body:'{}', credentials:'include'})

# セキュリティヘッダーの確認
curl -sI http://localhost:8080/api/me | grep -iE "cache-control|x-content-type|x-frame"
```

- `[SECURITY]` ブラウザのCookie値(session_id)を開発者ツールで別の値に書き換えると、以後のAPI呼び出しが401になる
- `[SECURITY]` 上記CSRFヘッダ無しのfetchを実行すると403になる
- `[SECURITY]` ログアウト後、ブラウザの「戻る」ボタンで直前のタスク一覧画面に戻ろうとしても、ログイン画面が表示される(古い一覧が一瞬でも見えない
  `Cache-Control: no-store`等のセキュリティヘッダーによる対策)
- `[SECURITY]` bff・bff-railsのレスポンスヘッダーに`Cache-Control: no-store` / `X-Content-Type-Options: nosniff` / `X-Frame-Options: DENY`が付与されている
- `[SECURITY]` 自分がログインしていない他ユーザーのタスクIDを直接指定してGET/PATCH/DELETEしても404になる(IDOR対策
  存在しないID、例: `999999`で確認できる)

#### backend多言語比較(Go / Rust / Scala×2 / Rails)

Architecture Delta(コア構成→多言語backend込み構成の差分レシート): [`docs/5_architecture-delta/bff-gin-core-vs-multilang.architecture-delta.html`](docs/5_architecture-delta/bff-gin-core-vs-multilang.architecture-delta.html)(コンポーネント追加4・変更1、接続追加4・変更1・経路変更1)
多言語backend込みの単体アーキテクチャ図: [`docs/5_architecture-delta/bff-gin-system-with-multilang.architecture.html`](docs/5_architecture-delta/bff-gin-system-with-multilang.architecture.html)

Task CRUD(内部REST/gRPC)を5言語で実装
`backend.task-language` / `backend.task-protocol`で切り替え

前提: Goのbackendが起動済みでマイグレーションが適用済みであること
他4言語は同じMySQLを読み書きするだけで、独自のマイグレーションは持たない

```sh
# backend.task-languageをgo→rust→scala-http4s→scala-pekko→railsの順に切り替える例
docker compose exec mysql mysql -uroot bff_gin_development -e \
  "UPDATE feature_flags SET default_variation='rust' WHERE flag_key='backend.task-language';"
# admin/go(http://localhost:8091)のFeature Flag編集画面から変更しても同じ
```

- 5.1節の手順で各言語のbackendを起動する
- 「どの言語が実際にリクエストを処理したか」は、bffの標準出力にある振り分けログ(`language`/`protocol`/`implementation`フィールド)、および各backend自身のリクエスト単位のログ(形式は言語ごとに異なる
  詳細は[各種ログの出力先](#各種ログの出力先)参照)の両方で確認できる
  各言語は互いに重複しないポートを専有しているため、対象言語プロセスが起動していなければ接続自体が失敗する(他の言語が代わりに答えることはない)
- `backend.task-language`を切り替え、そのつどfrontend(http://localhost:5173)からタスク一覧・作成・更新・削除を実際に操作し、どの言語を選んでも同じ形状で動くことを確認する(反映まで最大10秒ほどのポーリング待ちがある)
- `backend.task-protocol`をrest⇄grpcに切り替えても、5言語いずれでも同様に動く(最低限Go以外の1〜2言語で両プロトコルを確認すれば十分)
- `[SECURITY]` 他人のタスクIDを指定した削除で、Scala(http4s)・Scala(Pekko)ともラベル関連付けだけが消えてしまわないこと(所有者チェック前にラベル関連を無条件削除するIDOR類似脆弱性の回帰確認
  存在しないtask idや他ユーザーのtask idでDELETEを試し、404になり自分のタスクのラベルも消えていないことを確認する)
- `[SECURITY]` 期限(`finished_on`)を「今日の日付」に設定してタスクを作成すると、5言語のどのbackendでも一貫して受理される(「過去日付」判定は全言語UTC基準に統一されている
  日本時間の夜遅く〜深夜にかけて確認する価値がある)
- 確認後、`backend.task-language`を`go`・`backend.task-protocol`を`rest`に戻す(既定値)

#### 外部公開APIゲートウェイ(Go製 / nginx製)

:8081を占有し、`backend.task-language`に応じて外部公開APIを振り分ける(常にGoへフォールバック)

- 8.1節の手順でGo製またはnginx製ゲートウェイを起動する(同時起動不可)
- Keycloakでexternal-api-clientのトークンを取得し、:8081(ゲートウェイ)経由で`/external/v1/tasks`が呼べる(手順は次の「外部公開API」節と同じcurlで、ポートだけ:8081のまま)
- `backend.task-language`をrust等に切り替えても、外部公開APIは引き続き200を返す(Rust/Scala/Rails側にも外部公開APIが実装済みのため、実際にその言語が応答する
  ゲートウェイ自身のログに振り分け結果(`resolved_language`・`path`)が出る)
- `[SECURITY]` swagger-ui(http://localhost:18080)の「Try it out」から、ゲートウェイ経由(:8081)で`/external/v1/tasks`を実際に実ブラウザから叩ける(両ゲートウェイに`GATEWAY_ALLOWED_ORIGIN`によるCORS設定がある)
- 確認後、`backend.task-language`を`go`に戻し、ゲートウェイプロセスを停止する

#### frontend-rails(without-bff / with-bff)+ bff-rails

Railsで再現した2つの認証パターン
`omniauth-openid-connect` / `openid_connect`のgem切り替えも確認

```sh
# frontend-rails.oidc-gemの切り替え
docker compose exec mysql mysql -uroot bff_gin_development -e \
  "UPDATE feature_flags SET default_variation='openid_connect' WHERE flag_key='frontend-rails.oidc-gem';"
```

- 8.2節の手順でfrontend-rails/without-bffを起動する
  http://localhost:5174でKeycloakログイン→「ようこそ、〇〇さん」画面が表示され、「ログイン方式: keycloak」と表示される(Task一覧等のデータ表示は無し、ログイン確認のみのスコープ)
- `[SECURITY]` ログイン成功時、Railsのセッションが再発行されている(ログイン前のセッションIDが使い回されない
  Session Fixation対策)
- `frontend-rails.oidc-gem`を`openid_connect`に切り替えても、同様にログインできる
- frontend-rails/without-bffでもパスキーを登録・ログインできる(要: 事前にKeycloakでログイン済みであること
  welcome画面の「パスキーを登録する」リンクから`/account`へ進み登録→ログアウト→`/login`画面の「パスキーでログイン」からメールアドレス入力無しでログインできる
  backendの`webauthn_credentials`テーブルを共有するため、React+bff(Go)やbff-railsで登録したパスキーもそのままログインに使える)
- `[SECURITY]` Keycloakログイン→パスキー登録の一連の流れで、セッションCookieが4KB上限を超え`ActionDispatch::Cookies::CookieOverflow`が発生し、途中の画面へ遷移できなくなることがある(セッションCookieに`id_token`等を詰め込みすぎているのが原因
  Cookieストア側の見直しが必要な既知の課題)
- パスキーのみでログインした場合、welcome画面に「ログイン方式: passkey」と表示される
- 8.2節の手順でbff-rails + frontend-rails/with-bffを起動する
  http://localhost:5175でログイン→Task一覧が表示される(表示専用、作成/更新/削除は無し)
- bff-railsでもパスキーを登録・ログインできる(ただしbff-railsは登録時にBackup Eligible/Backup Stateフラグをbackendへ送っていないため、bff-railsで登録したパスキーを実機のクラウド同期パスキーでbff(Go)経由でログインしようとすると失敗する可能性がある既知の制約がある
  逆方向(bff(Go)登録→bff-railsログイン)は問題ない)
- React+bff(Go)・frontend-rails/without-bff・bff-railsのいずれで登録したパスキーも、互いにログインに使い回せる(backendの認証情報保存テーブルが共通のため)
- `[SECURITY]` 5分ほど放置(Keycloakのaccess_token失効)した後もTask一覧が取得できる(bff-railsのトークンリフレッシュ確認)
- 確認後、`frontend-rails.oidc-gem`を`omniauth-openid-connect`に戻し、起動した追加プロセスを停止する

#### 各種ログの確認

- backend/bff/admin/go/gatewayの標準出力に、リクエストログ(method/path/status)がJSON形式で出ている
- backend・bffのログにfeature flag評価ログ・振り分けログが出ている
- `LOG_LEVEL=debug`で起動すると、backendにGORMが発行したSQLログが追加で出る
- ブラウザの開発者ツール(コンソール)に、タスク一覧の新旧切り替えログ(`console.info`)が出ている
- 多言語backend(Rust/Scala×2/Rails)も、REST/外部公開API/gRPCそれぞれにリクエスト単位のログが出ている(形式・出力先は[各種ログの出力先](#各種ログの出力先)参照)

#### Redisの中身

セッション・分散ロック・ログイン中の一時状態

```sh
# ホストにredis-cliがある場合
redis-cli -h 127.0.0.1 -p 16379
# 無い場合はコンテナ内のredis-cliを使う
docker compose exec redis redis-cli

PING
KEYS *                                  # 全キー一覧(開発テスト用途のため使用可、本番のRedisでは使わないこと)
GET session:<session_idの値>            # ログイン中にKEYSで存在確認し、auth_modeがログイン方式と一致していることを見る
TTL session:<session_idの値>            # マイナスや-2ではないことを確認
KEYS lock:session:*                     # 分散ロックが今どのセッションで取得されているか
```

セッションIDはブラウザの開発者ツール(Application/Storage → Cookies)からsession_idの値をコピーして使う
ログアウト後、そのsession:{id}キーが消えていることも確認する
frontend-rails/without-bffはRedisではなくRailsのCookieセッション(`session[:auth_mode]`)で同等の情報を持つ点に注意(このセクションはbff(Go)・bff-rails向け)

#### Keycloakのログ・管理画面

```sh
docker compose logs keycloak | tail -30    # "Imported realm training" 等を確認
docker compose logs -f keycloak            # 追従表示
```

- 管理コンソール(http://localhost:8082)に`admin`/`admin`でログインできる
- realm "training"が存在し、クライアント`bff-gin` / `external-api-client` / `frontend-rails` / `bff-rails`が登録されている
- `bff-gin`・`bff-rails`・`frontend-rails`クライアントに`oidc-audience-mapper`(aud=backend)が設定されている
- Users画面に`general-user` / `admin-user`が存在する
- Sessions画面で、実際にログインした際にアクティブセッションが増えることを確認できる

#### 外部公開API(BFF非経由)

Client Credentials Grant、swagger-uiからも試せる
:8081はgateway経由、backend自身は:8097

```sh
# 1. Client Credentials Grantでトークンを取得
curl -s -X POST http://localhost:8082/realms/training/protocol/openid-connect/token \
  -d grant_type=client_credentials \
  -d client_id=external-api-client \
  -d client_secret=<realm-export.jsonの値> \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])"

# 2. 取得したaccess_tokenで呼び出す(gateway未起動なら直接backendの:8097へ)
curl http://localhost:8081/external/v1/tasks?user_id=1 \
  -H "Authorization: Bearer <access_token>"
```

- swagger-ui(http://localhost:18080)が開ける
- トークン無しで叩くと401になる
- `backend.external-tasks-pagination-v2`が`false`(既定)の場合は`page`/`page_size`によるoffsetページング、`true`の場合は`cursor`/`limit`によるキーセット(cursor)ページングになる(切り替えはadmin/go・admin/railsから)
- OpenAPI仕様は`backend/openapi/external-api.yaml`(Swagger UIで閲覧・Try it outできる)
- 既知の制約: `user_id`はクライアントが任意に指定できるため、このクライアント資格情報を持つ者は任意ユーザーのタスクを読み取れる(サーバー間の信頼関係を前提にした設計、CONTRACT.mdセクション11参照)

#### backend(Go)内でのORM比較(GORM / bob)

外部公開APIのTask一覧取得を、`backend.external-tasks-orm`フラグでGORM/bob([stephenafamo/bob](https://github.com/stephenafamo/bob))に切り替え

```sh
docker compose exec mysql mysql -uroot bff_gin_development -e \
  "UPDATE feature_flags SET default_variation='bob' WHERE flag_key='backend.external-tasks-orm';"
```

- `GET /external/v1/tasks`のレスポンスがgormのときと完全に同じ形状・同じ内容で返る(offset/cursor両方のページング方式で
  `backend.external-tasks-pagination-v2`と組み合わせて4パターン確認できると理想)
- backendの起動ログに、どちらのORM実装が使われたかの振り分けログが出ている
- ラベル付きのタスクでも、bob版で正しくラベルが取得できる(N+1にならない
  `task_labels`テーブルには外部キー制約が無く、bobの自動リレーション生成の対象外のためGORMのPreload相当の処理を手動実装している)
- 確認後、`backend.external-tasks-orm`を`gorm`に戻す(既定値)

## ディレクトリ構成

```
bff-gin/
  CONTRACT.md              # 設計の正本
 backend/ # REST v1 + gRPC v2(Go)、private network限定 feature_flagsテーブルの正本(マイグレーション)もここ
 backend-rust/ # Task CRUD Rust実装(追加構成) REST:8093 gRPC:9093 外部:8098
 backend-scala-http4s/ # Task CRUD Scala(http4s)実装(追加構成) REST:8094 gRPC:9094 外部:8099
 backend-scala-pekko/ # Task CRUD Scala(Pekko)実装(追加構成) REST:8095 gRPC:9095 外部:8100
 backend-rails/ # Task CRUD Rails実装(追加構成) REST:8096 外部:8101 gRPC:9096(3プロセス)
  bff/                     # OIDCクライアント・Redisセッション・Feature Flag(HTTP retriever)・プロキシ(Go)
 bff-rails/ # bffのRails版(追加構成) :8102
 frontend/ # React(Vite) features/tasksのみTypeScript
  frontend-rails/
 without-bff/ # bffを使わずRails自身がKeycloakと直接OIDCを行う比較実装(追加構成) :5174
 with-bff/ # bff-railsと組み合わせて使う比較実装(追加構成) :5175
  admin/
 go/ # Feature Flag管理画面+ユーザー管理画面(Go+Gin+html/template、Bootstrap) :8091
 rails/ # 同上のRails実装(比較用) :8092 goと同じMySQLテーブル・backend APIを操作する
  gateway/
 go/ # 外部公開APIゲートウェイ Go実装(追加構成) :8081
 nginx/ # 同上のnginx実装(追加構成) :8081(go版と排他)
  e2e/                     # playwright/ cypress/ selenium/ chromedp/ go-rod/ playwright-go/(計6フレームワーク)
 docs/ # C4図・ER図・シーケンス図(PlantUMLソース+PNG) _old/は過去バージョンのバックアップ
  docker-compose.yaml
```

## 各種ログの出力先

「言語によってログの有無・形式が異なる」ことを踏まえた一覧(CONTRACT.mdセクション20.10参照)
ファイル出力のものは実際の相対パスまで明記する

| 対象 | 出力先 | 形式 |
|---|---|---|
| backend(Go) | 標準出力(ターミナル) | JSON(`log/slog`) |
| bff | 標準出力(ターミナル) | JSON(`log/slog`) |
| admin/go | 標準出力(ターミナル) | JSON(`log/slog`) |
| gateway/go | 標準出力(ターミナル) | JSON(`log/slog`) |
| backend-rust | 標準出力(ターミナル) | key=value形式(`tracing`) |
| backend-scala-http4s | 標準出力(ターミナル) | logbackのデフォルト形式(設定ファイル無し、DEBUGレベル) |
| backend-scala-pekko | 標準出力(ターミナル) | logbackのデフォルト形式(設定ファイル無し、DEBUGレベル) |
| backend-rails | ファイル: `backend-rails/log/development.log`(test実行時は`log/test.log`) | Rails標準ログ形式(REST/外部公開API) gRPC(`bin/grpc_server`)は独自のkey=value形式で同じファイルへ追記 |
| admin/rails | ファイル: `admin/rails/log/development.log`(test実行時は`log/test.log`) | Rails標準ログ形式 |
| frontend-rails/without-bff | ファイル: `frontend-rails/without-bff/log/development.log`(test実行時は`log/test.log`) | Rails標準ログ形式 |
| frontend-rails/with-bff | ファイル: `frontend-rails/with-bff/log/development.log`(test実行時は`log/test.log`) | Rails標準ログ形式 |
| bff-rails | ファイル: `bff-rails/log/development.log`(test実行時は`log/test.log`) | Rails標準ログ形式 |
| gateway/nginx | ファイル: `gateway/nginx/logs/access.log`・`logs/error.log` | nginx標準形式 |

いずれもファイル出力のもの(Rails各アプリ・nginx)以外は、ターミナルを閉じる/プロセスを止めるとログも消える

### backend多言語比較: リクエスト単位のログの精度(CONTRACT.mdセクション20.10・20.11)

REST・外部公開APIは5言語とも当初から正確(外側のHTTPステータスがそのまま実際のステータスのため)
gRPCは「外側のHTTPステータスが常に200固定」という性質上難しかったが、現在は5言語全てが実際のステータスコードまで正確に記録する

| 言語 | 内部REST/外部公開API | gRPC: ログの有無 | gRPC: 実際のステータスコードまで正確か | 実装方法(gRPC) |
|---|---|---|---|---|
| Go | ✅ 正確(実HTTPステータス) | ✅ あり | ✅ 正確(`status.Code(err)`) | `grpc.ChainUnaryInterceptor`が`err`を直接受け取る |
| Rust | ✅ 正確 | ✅ あり | ✅ 正確(`tonic::Status::code()`) | `LoggingTaskGrpcService`デコレータ(tonic生成traitを直接ラップ) |
| Scala(http4s) | ✅ 正確 | ✅ あり | ✅ 正確(`Status.getCode`) | `io.grpc.ServerInterceptor` |
| Scala(Pekko) | ✅ 正確 | ✅ あり | ✅ 正確(`io.grpc.Status`) | `LoggingTaskGrpcService`デコレータ(pekko-grpc生成traitを直接ラップ) |
| Rails | ✅ 正確(Rails標準ログ) | ✅ あり | ✅ 正確(`GRPC::BadStatus#code`) | `GRPC::BadStatus#code`を直接記録 |

いずれも「ハンドラの型付き戻り値/例外を直接見る」実装方式(HTTP/2トレーラーを直接読み取る実装は、自分でハンドラを実装していない汎用ミドルウェア・OpenTelemetry計装等でのみ必要な手段であり、このプロジェクトではハンドラを自前で持っているため不要と判断した)
Scala(Pekko)には既知の制約があり、REST/外部公開APIでルートに一切マッチしない404相当のパスは`status=rejected`と表示される(実際のステータスコードではない)

## 関連ドキュメント

- 設計契約: [`CONTRACT.md`](./CONTRACT.md)
- C4図・ER図・シーケンス図(PlantUMLソース・PNG): [`docs/`](./docs/)配下(`c4_context_v2` / `c4_container_v2` / `c4_component_v2` / `er_diagram_v2` / `sequence_login_v2` / `sequence_task_create_v2` / `sequence_token_refresh` / `sequence_logout` / `sequence_feature_flag_routing` / `sequence_external_api`)
- `docs/_old/`は過去バージョンのバックアップ(参照専用、更新しない)
- 動作確認のチェックリスト(Markdown版): [`VERIFICATION_CHECKLIST.md`](./VERIFICATION_CHECKLIST.md)(本READMEの「確認手順」と同内容をチェックボックス形式で管理したもの)

## その他、検討事項など

以下は設計検討時のQ&Aメモ
①・⑤は検討段階で挙げていた論点だったが、その後「ローカル(非Keycloak)認証」として実装済みのため、ここでは結論だけ記す
②③④は今回採用していない選択肢についての一般的な整理であり、検討メモとして残す

**① Keycloakなしのログイン画面 → 実装済み**

`/login`(HMAC版)・`/login/rsa`(RSA版)の2方式
詳細は前述の「ローカル(非Keycloak)認証」を参照

**② 「ブラウザにトークンを返さない」と「ステートレス」は矛盾しないか**

「ステートレス」という言葉が2つの異なる意味で使われていることに起因する、見かけ上の矛盾

- 今回の設計での「ステートレス」の意味: BFFのプロセス自体がメモリ内にセッションを持たない、という意味
  セッションの実体(Access/Refresh/ID Token)はRedisという「BFFプロセスの外側」に置き、BFFはただの「Redisへの参照(session_idというCookie)を検証する薄い層」になる
  これによりBFFインスタンスを何台に増やしても同じRedisを見るので問題なく動く、という水平スケーラビリティのための設計
- 一般に言う「ステートレス認証」(狭い意味): JWTのような自己完結型トークンをそのままブラウザに持たせ、サーバー側は署名検証だけで済ませ、セッションストアへの問い合わせが一切不要、という意味で使われることが多い

今回の設計は後者の意味では厳密には「ステートレス」ではない(毎リクエストRedisへの参照が必要)
正確には「ステートレスなBFFプロセス」+「ステートフルな認証システム全体(状態はRedisに存在する)」という組み合わせであり、矛盾ではなくトレードオフの選択である

- トークンをブラウザに渡す(狭義のステートレス): XSSでの窃取リスク、失効させたくても自己完結トークンなので即時無効化が難しい(結局ブロックリストという「状態」が必要になりがちで、完全なステートレスは実務では稀)
- トークンをブラウザに渡さない(BFFパターン): 安全だが、必ずどこかに「session_id→トークン」の対応表(=状態)を持つ必要がある(今回はそれをRedisに外出しした)

「ブラウザに一切トークンを渡さず、かつ、どこにも状態を持たない」という両立は原理的に不可能

**③ Keycloak以外のGo製OAuth/OIDCライブラリ、Keycloak無しのベストプラクティス**

クライアント側(bffの立場)のコード(`coreos/go-oidc/v3` + `golang.org/x/oauth2`)は既にKeycloak専用ではなく、OIDC Discoveryの標準仕様に従っているので、`OIDC_ISSUER_URL`をKeycloak以外(Auth0、Okta、Google、Zitadel等)に向けるだけでそのまま動く

IdP(サーバー)を自前で用意する場合のGo製選択肢:

| ライブラリ/製品 | 位置づけ |
|---|---|
| `ory/fosite` | OAuth2/OIDCサーバーを自分で組み立てるためのフレームワーク(Ory Hydraの内部エンジン) プロトコル実装の学習には最適だが本番品質に持っていくのは大変 |
| `go-oauth2/oauth2` | より軽量なOAuth2サーバー実装ライブラリ OIDC(id_token)は自分で足す必要がある |
| Zitadel / Dex | Go製の既製IdP(ライブラリではなくアプリケーション) |
| `golang.org/x/oauth2/google`等 | OIDCではなく単純な「Googleでログイン」等の素朴なOAuth2連携が目的ならこちら |

「フルのOIDC仕様が本当に必要か」を再確認するのが第一歩
今回のような学習で「ログイン状態を保持できればよい」程度なら、IdPを自作/導入せず、bffが直接ID/パスワードを検証しセッションを発行する(①の比較実装がこれに相当)方が圧倒的にシンプル
複数クライアント(モバイルアプリ等)への展開やSSOが本当に要件にあるときだけ、Zitadel/Dexのような軽量IdPを検討する、という優先順位が実務的である

**④ サイドカー構成での認証パターン**

「BFFを1つの集約サービスとして立てる」のではなく、各サービスの前に認証専用のプロキシを1台ずつ(Kubernetesなら同一Pod内に)配置する構成

| 技術 | 特徴 |
|---|---|
| oauth2-proxy | 最も普及しているOSS OIDC/OAuth2のAuthorization Codeフロー・セッションCookie管理・Redisバッキングストア対応まで、今回bffで手書きした処理を設定だけでやってくれる |
| Envoy + ext_authz | Envoyサイドカーが外部認可サービスにリクエストを問い合わせてから上流へ通す Istio等のサービスメッシュでよく使われる |
| Istio RequestAuthentication/AuthorizationPolicy | JWT検証をアプリコードに一切書かず、メッシュ(サイドカー)側で宣言的に検証・拒否する ゼロトラスト思想 |
| Pomerium | oauth2-proxyより高度なポリシー機能を持つIdentity-Aware Proxy |

構成イメージ(Kubernetes Pod内): `[oauth2-proxyサイドカー] :4180 ← 外部公開` → 認証済みリクエストのみlocalhost経由で転送 → `[本来のbackendコンテナ] :8080 ← Pod外からは直接到達不可`

BFF集中型(今回の構成)はサービスが1つ(または少数)の場合にシンプル
サイドカー型は「多数のマイクロサービスそれぞれに同じ認証ロジックを重複させたくない」「サービスメッシュを既に導入していてmTLS等と一緒に認証も乗せたい」場合に真価を発揮する
今回のプロジェクト規模(フロント1つ・backend1つ)ではBFF集中型で十分だが、比較としてoauth2-proxyをbackendの前に置く構成を別途用意することもできる

**⑤ 自前実装のログイン機能を追加する際、backendのJWT検証が複数パターンに対応する必要がある問題 → 実装済み**

`authjwt.Dispatcher`が`iss`クレームでKeycloak/`bff-gin-local-hmac`/`bff-gin-local-rsa`の3方式を振り分け、`RequireAuth`/`RequireExternalClientAuth`はこの`Dispatcher`経由で検証する
詳細は前述の「ローカル(非Keycloak)認証」を参照
