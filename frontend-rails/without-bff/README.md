# frontend-rails/without-bff

CONTRACT.md参照:
既存の`frontend`(React) + `bff`(Go)の「BFFパターン」
(トークンはbffだけが持ち、Reactはbffが発行する不透明なセッションCookieしか見ない)
と対照的に、
**RailsアプリがOIDCクライアントとして自分自身でKeycloakとAuthorization Codeフローを直接行い、Railsの標準セッションだけで完結する**
構成の比較実装
bffを一切経由しない

スコープは意図的にシンプルな認証確認画面のみ(ログイン→「ようこそ、〇〇さん」表示→ログアウト)
Task 一覧等のデータ表示は無く、MySQL・backend への接続も一切行わない
(唯一の例外は後述のFeature Flag ポーリング、これもbackendのHTTP exportエンドポイントを叩くだけでMySQLへは繋がない)

## セットアップ

```sh
cd frontend-rails/without-bff
bundle install
```

## 起動

```sh
# training-go/bff-gin ルートで
docker compose up -d --wait mysql keycloak
cd backend && go run ./cmd/migrate up && go run ./cmd/server &   # flag export用(:8090)

cd frontend-rails/without-bff
bin/rails server -p 5174
```

`http://localhost:5174/login` から、Keycloakの`general-user`/`admin-user`(パスワード`password`) でログインできる

## Feature Flag: `frontend-rails.oidc-gem`(2値、gem切り替え)

OIDCハンドシェイクの実装をまるごと切り替えるRelease Toggle
admin/go・admin/rails の画面、または MySQL の `feature_flags` テーブルを直接操作して切り替える

| 値 | 実装 |
|---|---|
| `omniauth-openid-connect`(既定) | `omniauth` + `omniauth_openid_connect` gem。Rackミドルウェア(`OmniAuth::Builder`)がstate/nonce/PKCE/リダイレクト/コールバック処理を自動で面倒見る「お任せ型」 |
| `openid_connect` | `openid_connect` gem単体(低レベルライブラリ)。認可リクエストの組み立て・state/nonce/PKCEの生成と検証・code→tokenの交換・ID Tokenの署名検証(JWKS)を全て`app/services/manual_oidc_client.rb`と`SessionsController`で手組みする |

flagの値は、backendの`GET /internal/v1/feature-flags/export`を`app/services/oidc_gem_flag.rb`が
10秒間隔でポーリングして取得する
(bff・gatewayと同じ「exportエンドポイントをポーリングする」方式MySQLへの直接接続は増やさない)

値はプロセス内でスレッドセーフにキャッシュされ、エクスポートAPIに到達できない場合は既定値(`omniauth-openid-connect`)にフォールバックする

**コールバックパス(`/auth/openid_connect/callback`)はどちらのgemでも同じ**にしている
(Keycloak 側の redirect_uri 登録を1つに保つため)
これを実現するため、`app/middleware/oidc_gem_dispatcher.rb`というRackミドルウェアが、
リクエストごとに現在のflag値を見て、`OmniAuth::Builder`へ処理を渡すか(お任せ型)、
Railsルーティングへそのまま素通しして`SessionsController#start_login`/`#omniauth_callback`に処理させるか(手組み型)を出し分けている

## テスト

```sh
bundle exec rspec
```

両gem実装それぞれのログイン/コールバック/ログアウト/失敗ケース、Feature Flagのポーリングロジックをカバーする(計16件)
手組み(`openid_connect`)側のテストは、実際にRSA鍵ペアで署名した ID Token を使い、
WebMock で偽Keycloak(discovery・JWKS・token endpoint)をスタブして、署名検証まで本物同様に通している

## 動作確認(実機)

docker-compose上の実際のKeycloak(`general-user`/`password`)に対して、**どちらのgem実装でも**
以下を`curl`で最初から最後まで実行し、ログイン→ID Token検証→セッション作成→ログアウト
(RP-Initiated Logout、実際のid_token_hint付き)まで確認済み

1. `POST /auth/openid_connect`(CSRFトークン込み)→Keycloakの実際の認可エンドポイントへ302
2. Keycloakのログインフォームへ`general-user`/`password`をPOST→実際の認可コード付きで
   `/auth/openid_connect/callback`へ302
3. コールバックURLへアクセス→`/welcome`へ302、実際に「ようこそ、一般 ユーザーさん」を表示
4. `DELETE /logout`→Keycloakの`end_session_endpoint`へ302(手組み側は実際のid_token_hintを
   URLに含めることまで確認)

## 実装時に見つかった実際の不整合

- **PKCE抜け(手組み実装のバグ、修正済み)**:
  - `bff/keycloak/realm-export.json`の`frontend-rails`クライアントは`pkce.code.challenge.method=S256`が設定されているため、
  - PKCE無しの認可リクエストは Keycloak から`invalid_request(Missing parameter: code_challenge_method)`で即座に拒否される
  - `omniauth-openid-connect`側は`pkce: true`オプションが自動でcode_verifier/code_challengeを生成してくれるため気づきにくいが、
  - `openid_connect`gem単体ではPKCEは完全に自前で実装する必要がある
  - (`code_verifier`生成 → `code_challenge = base64url(sha256(code_verifier))`
  - → 認可リクエストに`code_challenge`/`code_challenge_method`を付与
  - → セッションに`code_verifier`を保存 → token交換時にそのまま送る、という一連の手順)
  - これは実機の `curl` 確認で初めて発覚
    - 単体テストでは Keycloak の実際の PKCE検証を経由しないため気づけなかった
- **`JSON::JWK`のkidアクセス**:
  - テストヘルパーで`jwk.kid`というメソッド呼び出しは存在せず、`jwk[:kid]`(Hashライクなアクセス)が正しい
- **`app/middleware/`・`app/services/` は config/initializers/* から見るとまだオートロード対象外**:
  - Rails 8 の Zeitwerkオートローダーは、`config/initializers/*`が読まれる時点ではまだ完全にセットアップされておらず、
  - 初期化処理(`config.middleware.use`)がその場で必要とするクラスへの参照は`NameError(uninitialized constant)`になる
  - 該当クラスをその初期化ファイル内で明示的に`require_relative`することで解決した
  - クラス内部のメソッド本体からの参照はリクエスト処理時になるため、オートローダー任せで問題ない

## omniauth-openid-connect vs openid_connect、実装してみての所感

- **お任せ型(omniauth-openid-connect)は圧倒的に書く量が少ない**:
  - `provider :openid_connect, ...`の設定を書くだけで、discovery・state/nonce/PKCE・コールバック処理・エラーハンドリングまでミドルウェアが面倒を見てくれる
  - 今回のように「同じアプリの中で2つの実装を共存させ、ミドルウェアを動的にon/offする」ような特殊なことをしない限り、実務ではまずこちらを選ぶべき
- **手組み型(openid_connect)は「何が起きているか」が全部見える代わりに、地雷も多い**:
  - PKCEの実装漏れのように、標準に沿ったつもりでも実は必須パラメータが抜けている、という事故が起きやすい
  - 一方で、discoveryドキュメントの取得・JWKSによる署名検証・state/nonceの検証を全部自分のコードで書くため、
  - OIDC の仕組み自体を理解する教材としては圧倒的に学びが多い
- **ミドルウェアの動的な出し分けは、omniauth特有の事情に起因する複雑さ**:
  - OmniAuth は Rackミドルウェアとしてリクエストパスを横取りする設計のため、
  - 「同じパスを2つの実装で共存させる」には`OidcGemDispatcher`のような外側の出し分けが要る
  - これは omniauth 側の設計に起因するもので、仮に両方とも手組み実装
  - (Railsコントローラのアクションとして素直に書く)
  - にしていれば、Feature Flag で単純に`if`分岐するだけで済んでいたはず
