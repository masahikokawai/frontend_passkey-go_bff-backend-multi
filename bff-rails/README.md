# bff-rails

`frontend-rails/with-bff` 専用のBFF(Backend for Frontend)。既存の `bff`(Go+Gin)と全く同じ役割
(Keycloakとの OIDCハンドシェイク・セッション発行・backendへのアクセス代行)を、Ruby on Railsで
再現した比較実装。「クライアントごとに専用のBFFを持つ」というベストプラクティス
(将来モバイルアプリ等が増えても、backendは常にBFF経由のみで公開する)の2つ目の実例でもある。

## 役割分担

```
ブラウザ ⇄ frontend-rails/with-bff(薄いクライアント、:5175)
              │ fetch(credentials: include)
              ▼
          bff-rails(このアプリ、:8102) ⇄ Keycloak(:8082)
              │ Bearer aud=backend token
              ▼
          backend(:8090, 既存Go実装の /internal/v1/*)
```

ブラウザは `bff-rails` が発行する不透明なセッションCookie(`bff_rails_session`)しか持たず、
アクセストークン等の実データはRedis(bffと同じくRedisをセッションストアに使う)にだけ保持する
(BFFパターンの核心、既存`bff`の`internal/auth/session.go`と同じ設計)。

## セットアップ

```sh
cd bff-rails
bundle install
```

## 実行

```sh
# training-go/bff-gin ルートで
docker compose up -d --wait mysql redis keycloak
cd backend && go run ./cmd/migrate up && go run ./cmd/server &

cd bff-rails
bin/rails server -p 8102
```

## 環境変数

| 変数 | 既定値 |
|---|---|
| `HTTP_ADDR` | `8102` |
| `REDIS_URL` | `redis://127.0.0.1:16379/0` |
| `KEYCLOAK_ISSUER` | `http://localhost:8082/realms/training` |
| `KEYCLOAK_CLIENT_ID` | `bff-rails` |
| `KEYCLOAK_CLIENT_SECRET` | `bff-rails-local-dev-secret` |
| `REDIRECT_URI` | `http://localhost:8102/auth/openid_connect/callback` |
| `POST_LOGOUT_REDIRECT_URI` | `http://localhost:5175/` |
| `FRONTEND_ORIGIN` | `http://localhost:5175`(CORS許可オリジン) |
| `BACKEND_REST_BASE_URL` | `http://localhost:8090` |
| `FEATURE_FLAG_EXPORT_URL` | `${BACKEND_REST_BASE_URL}/internal/v1/feature-flags/export` |
| `FEATURE_FLAG_POLL_TOKEN` | `local-dev-feature-flag-poll-token` |

## エンドポイント

- `GET /api/auth/login` — Keycloakへリダイレクト(PKCE付き)
- `GET /auth/openid_connect/callback` — 認可コード交換→ID Token検証→JITプロビジョニング→セッション発行
- `GET /api/me` — ログイン中ユーザーのname/email
- `GET /api/tasks` — backendの`/internal/v1/tasks`をそのまま透過して返す(表示専用)
- `POST /api/auth/logout` — セッション削除、Keycloak RP-Initiated LogoutのURLを返す
- `POST /api/auth/passkey/register/begin` / `finish` — パスキー登録(要ログイン、下記参照)
- `POST /api/auth/passkey/login/begin` / `finish` — パスキーログイン(公開、下記参照)

## パスキー(WebAuthn)対応

CONTRACT.mdセクション22の追加実装。既にKeycloakでログイン済みのユーザーが、追加の認証手段として
パスキーを登録できる。登録(`register/*`)は要ログイン、ログイン(`login/*`)はパスキー自体が
Keycloakを経由しない認証手段のため公開エンドポイント。

「webauthn」という技術用語だと何を登録するエンドポイントか分かりにくいため、パスには`passkey`を
使う命名規則にした(ユーザーからの明示的な指定)。

### 重要な設計: backendの内部APIをbff(Go)と完全に共有する

パスキーの資格情報(`webauthn_credentials`テーブル)は、bff(Go)側の実装と**全く同じ**backendの
内部API(`POST/GET/PATCH /internal/v1/auth/webauthn/credentials/*`)を呼ぶ。新規のbackend側実装は
一切していない。この設計により、**Reactの既存frontend+bff(Go)で登録したパスキーが、そのまま
bff-railsのログインにも使い回せる**(同じbackendの`webauthn_credentials`テーブルを参照するため)。
実機検証では、bff-rails単体で登録したパスキーがbackendの`GET /internal/v1/admin/users`から
`has_passkey: true`として見えることを確認した(bff(Go)がこのタイミングで起動していなかったため、
bff(Go)側からの実ログインまでは確認できていないが、資格情報が特定のBFF実装に紐づかず
backend側に共有されていることの構造的な証明にはなっている)。

rp_id(WebAuthnのRelying Party ID、資格情報のスコープを決める値)はbff(Go)側と同じ`"localhost"`
(ポート番号を含まないドメインのみ)に揃えている。これにより、同一ブラウザ上でどちらのBFF
(`:5173`のReact経由でも`:5175`のfrontend-rails/with-bff経由でも)からでも、同じ「localhost用の
パスキー」が選択可能になる(originの検証はBFFごとに自分の想定オリジンで別々に行う)。

### Ruby実装(`webauthn` gem)を使ってみて見つかった実際の問題

- **`WebAuthn.configure`ブロックは即座に評価される**(既存の`Rack::Cors`設定DSLと違い遅延評価では
  ないため)、Zeitwerkのオートロードが間に合わないタイミングで`AppConfig`を参照すると
  `NameError: uninitialized constant AppConfig`になった。`config/initializers/webauthn.rb`内で
  明示的に`require_relative`することで回避(既存の`app/middleware`系初期化子と同じ既知の回避策)
- **`sign_count`は素のIntegerではなく`BinData::Bit32`**を返す。`.to_json`にそのまま渡すと、
  BinDataが遅延評価する内部の長さフィールド解決に失敗し
  `NoMethodError: undefined method 'trailing_bytes_length' for an instance of BinData::LazyEvaluator`
  という分かりにくいエラーで落ちる。`.to_i`で明示的に素のIntegerへ変換してから使う必要がある
  (`credential.id`もStringのはずだが、念のため`.to_s`で防御的に変換している)
- **`WebAuthn::Credential.from_create`/`from_get`は文字列キーのHashを要求する**
  (`credential["id"]`のように直接ブラケットアクセスするため)。Railsの`params.permit!.to_h`は
  素朴に呼ぶとシンボルキー化されがちで、`deep_symbolize_keys`すると`credential["id"]`が常に`nil`に
  なり「invalid id」で必ず検証失敗する。`deep_stringify_keys`を使うこと
- **テストには`WebAuthn::FakeClient`(gem本体が提供するテスト用ヘルパー)が使え、`.verify()`を
  モックせず本物の暗号検証を実際に通すテストが書けた**。Goのgo-webauthnにも同種のテストヘルパーが
  あるかは未確認だが、Rubyのwebauthn gemはこの点で開発体験が良かった

### 解決済み: パスキーログイン後も`/api/tasks`が使える

以前は、パスキーログインがKeycloakを経由しないため`access_token`(aud=backend付き)を
持たず、`GET /api/tasks`が502になる制約があった。これは`Auth::LocalBackendTokenIssuer`
(bff(Go)の`internal/auth/local_jwt.go`と同じ`iss=bff-gin-local-hmac`・HS256・共有シークレット
方式)を追加し、パスキーログイン成功時にこの自前JWTを発行してセッションの`access_token`
フィールドへそのまま収めることで解決した。backend側の`authjwt.HMACVerifier`は発行元が
bff(Go)かbff-railsかを区別しないため、**backend側のコード変更は一切不要**だった。
`TasksController`・`BackendClient`も「access_tokenが何によって発行されたか」を意識しないため
無変更で動く。

## OIDCハンドシェイクのgem切り替え(Feature Flag `frontend-rails.oidc-gem`)

`frontend-rails/without-bff`と全く同じFeature Flag(5言語共有ではなく、この用途専用の2値flag)
で、以下2つのgem実装を切り替える。値はbackendの`/internal/v1/feature-flags/export`を10秒間隔で
ポーリングして取得する(admin/go・admin/rails等のようにMySQLへ直接繋ぐ経路は増やしていない)。

- **`omniauth-openid-connect`**(既定): `OmniAuth::Strategies::OpenIDConnect`が持つ`client`
  (`OpenIDConnect::Client`)構築ロジック・設定規約(`client_options`/`issuer`)をそのまま使う
- **`openid_connect`**: `OpenIDConnect::Client`を自分で直接組み立てる「手組み型」

### 【設計上の注記】omniauth-openid-connectをRackミドルウェアとして使わなかった理由

`omniauth-openid-connect`は本来Rackミドルウェアとして常駐させ、ブラウザのRackセッションに
state/nonceを保存する設計だが、bff-railsはAPI-onlyでRackセッションを使わない(BFFパターンとして
Redisだけに状態を持つ設計のため)。加えて、Feature Flagで実行時にgemを切り替える以上、
「ミドルウェアを起動時に固定で1つだけ組み込む」という前提とも相性が悪い。そのため
`OmniAuth::Strategies::OpenIDConnect`のインスタンスは「`client`ビルダー」としてのみ使い、
state/nonce/code_verifierは自前の`Auth::PendingLoginStore`(Redis、TTL 5分)で管理している。

## 実装時に見つかった実際の非互換性(全て修正済み)

1. **`omniauth-openid-connect`(jjbohn版、2020年以降更新停止)が`openid_connect ~> 0.9.2`に
   固定依存しており、2015年頃の古いバージョンしか使えない**。この`openid_connect 0.9.2`は
   Rails 4時代の`ActiveSupport#alias_method_chain`(Rails 5で削除済み)を使っており、
   現行Railsではgemのrequire自体が`NoMethodError`で失敗する
   → `config/application.rb`で`alias_method_chain`を復元する最小限のshimを追加(古いgemを
   現行Railsで動かす際によく使われる実務的な回避策)
2. **`json` gem 3.0系とActiveSupport 8.0.5.1の非互換**: `JSON.generate`が`quirks_mode`
   キーワードを受け付けなくなっており、`to_json`呼び出しが軒並み`ArgumentError`で落ちる
   → `json ~> 2.9`に固定
3. **`rack-oauth2` 2.3.0(現行)と`openid_connect` 0.9.2(上記の理由で固定)の非互換**:
   `Rack::OAuth2.http_client`の既定Faradayコネクションに`faraday.response :json`
   ミドルウェアが入っており、`response.body`が既にHashへパース済みになる。しかし
   `openid_connect 0.9.2`の`Client#handle_success_response`はさらに`JSON.parse(response.body)`
   を呼ぶ実装のままで、`TypeError: no implicit conversion of Hash into String`で必ず落ちる
   → `OpenIDConnect::Client#handle_success_response`を、bodyが既にHashなら
   そのまま使うよう`prepend`で上書きする互換シムを追加(`config/initializers/openid_connect_compat.rb`)
4. **PKCE必須**: Keycloak側の`bff-rails`クライアントは`pkce.code.challenge.method: S256`が
   設定されており、`code_challenge`が無いと`invalid_request: Missing parameter:
   code_challenge_method`で認可リクエスト自体が拒否される
   → `Auth::PendingLoginStore`でcode_verifierも生成・保管し、認可URLに`code_challenge`を
   付与、token交換時に`code_verifier`を渡すよう実装(RFC 7636)
5. **ログアウトの実装ミス**(このアプリ自体のバグ、テストで発見): `cookies.delete`を
   `session_id_from_cookie`より先に呼んでいたため、Cookie削除後に読んだセッションIDが常に
   `nil`になり、`SessionStore.destroy`が何もしないno-opになっていた。Cookieを読んでから
   削除する順序に修正

## 実機確認について

docker-compose上のMySQL・Redis・Keycloakと、実際のbackend(Go)を起動した状態で、
Keycloakのログインフォームへの実際のPOST(curlでcookie jarを使いフォーム送信→リダイレクトを
手動で追跡)まで含めて、以下を実際に確認済み。

- `omniauth-openid-connect`/`openid_connect`いずれのflag値でも、ログイン→PKCE付き認可リクエスト
  →Keycloakでの実認証(`general-user`/`password`)→コード交換→ID Token検証→JITプロビジョニング
  (backendの`/internal/v1/users/provision`)→セッションCookie発行、まで一貫して成功
- 発行されたセッションで`GET /api/me`・`GET /api/tasks`が実際のbackendデータ(既存のe2e等で
  作成された実タスク)を返すことを確認
- `POST /api/auth/logout`後、同じCookieで`/api/me`が401になることを確認

## テスト

```sh
bundle exec rspec
```

20件、全てpass(feature flag取得・PKCE・state検証・ID Token署名検証を含むコード交換の
成功/失敗パス・JITプロビジョニング・セッションCRUD・ログアウト)。

## Go実装(bff)との比較所感

- **セッションストア**: Go版と全く同じ「Redis + 不透明なCookie値」方式を踏襲できた。
  Railsの標準セッション(CookieStore)ではなく、あえて生のCookie値を1つ読み書きするだけの
  薄い実装にした方が、bffの設計と対比しやすかった
- **CORS**: GoではGinミドルウェアで書いていたものが、`rack-cors`でほぼ同じ量のコードになる。
  大きな差は感じなかった
- **gemのバージョン互換性の脆さ**: 上記の非互換性3件は、Goのモジュールエコシステム
  (セマンティックバージョニングが厳格で、ビルド時に依存グラフ全体が解決される)では
  あまり踏まない類の罠だった。Rubyのgemは実行時にrequireされるものが多く、
  「インストールはできたが実際にrequireすると壊れる」という組み合わせが起こり得ることを
  実感した
- **PKCEの要求元**: これはgemの問題ではなくKeycloak側のクライアント設定(既存の
  `bff-gin`と同じ設定を踏襲したため)によるもので、Go版のbffは(おそらく使っているOIDC
  クライアントライブラリが自動でPKCEに対応していたため)特に意識せず動いていた。
  Railsで同じことを実現するには、自分でcode_verifier/code_challengeを生成・保管する
  実装が必要だと分かったのは良い学びだった
