// Package config は環境変数からBFFの設定を読み込む
// Rails: config/application.rb + ENV は各Controllerが直接ENVを読むのに対し、
// Goでは起動時に1箇所へ集約して構造体化しておくことで、設定漏れをコンパイル時の
// 構造体アクセスミスとして検出しやすくする
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Config はBFFプロセス全体で共有する設定値
type Config struct {
	// Port はBFFがHTTPで待ち受けるポート。Keycloakのクライアント設定
	// (bff/keycloak/realm-export.json の redirectUris)がこの値(既定8080)を
	// 前提にしているため、変更する場合はrealm-export.jsonも合わせて直すこと
	Port string

	// RedisAddr はセッションストア(Redis)の接続先
	// 既定値はdocker-compose.yamlでホストへ公開しているポート(127.0.0.1:16379)
	// bffはコンテナ化せずネイティブ実行する前提のため、dockerサービス名
	// (redis:6379)ではなくホスト側の公開ポートを指す値にしている
	RedisAddr string
	RedisDB   int

	// Keycloak関連。IssuerURLは `{IssuerURL}/.well-known/openid-configuration` を
	// 取得してauthorization/token/end_sessionエンドポイントを解決する起点になる
	// 既定値はdocker-compose.yamlのKC_HOSTNAME=localhost/KC_HOSTNAME_PORT=8082、
	// およびrealm-export.jsonのbff-ginクライアント定義に対応する固定の開発用値
	OIDCIssuerURL      string
	OIDCClientID       string
	OIDCClientSecret   string
	OIDCRedirectURL    string // {BFF公開URL}/api/auth/callback
	PostLogoutRedirect string // ログアウト後にKeycloakから戻す先(フロントのトップ等)

	// FrontendBaseURL はログイン成功後にリダイレクトするReact SPAのオリジン
	// 【実機検証で判明】/api/auth/loginの`redirect`クエリはfrontendが渡す相対パス
	// (例: "/tasks")でしかない。これをbffのCallbackがそのままc.Redirectに渡すと、
	// ブラウザは現在のオリジン(bffの:8080)からの相対パスとして解釈してしまい、
	// http://localhost:8080/tasks へ遷移して「404 page not found」になる
	// (bffのGinルーターに/tasksというルートは無いため)。そのためCallbackでは
	// この値と相対パスを連結し、必ずfrontendのオリジンへ絶対URLでリダイレクトする
	FrontendBaseURL string

	// BackendRESTBaseURL は backend v1(REST)への接続先
	// backendもネイティブ実行する前提のため既定値は http://localhost:8090
	// (backendのHTTPAddr既定値と一致させる。CONTRACT.mdに書いていた:8080は
	// 両者をコンテナ化する前提の値で、bffの既定ポート:8080と衝突するため
	// 実機検証で:8090に変更した)
	BackendRESTBaseURL string
	// BackendGRPCAddr は backend v2(gRPC)への接続先
	BackendGRPCAddr string

	// CONTRACT.mdセクション20: backend.task-language(go/rust/scala-http4s/scala-pekko/rails)
	// ごとの接続先。TaskRoutes.Clientsのキー("{language}:{protocol}")と対応させて
	// main.goで組み立てる。いずれも各言語実装のREADMEに記載のポートに合わせた既定値
	RustRESTBaseURL        string
	RustGRPCAddr           string
	ScalaHTTP4sRESTBaseURL string
	ScalaHTTP4sGRPCAddr    string
	ScalaPekkoRESTBaseURL  string
	ScalaPekkoGRPCAddr     string
	RailsRESTBaseURL       string
	RailsGRPCAddr          string
	JSRESTBaseURL          string
	JSGRPCAddr             string
	TSRESTBaseURL          string
	TSGRPCAddr             string

	// FeatureFlagFilePath はGO Feature Flagのフラグ定義YAMLのパス(旧方式、参考用
	// 実際にはFeatureFlagExportURLが使われる。internal/featureflag/flags.yaml参照)
	FeatureFlagFilePath string

	// FeatureFlagExportURL はbackendの GET /internal/v1/feature-flags/export のURL
	// GO Feature FlagのHTTP retrieverがこのURLを定期ポーリングする(CONTRACT.mdセクション13)
	FeatureFlagExportURL string
	// FeatureFlagPollToken はFeatureFlagExportURLへのリクエストに付与する共有シークレット
	// (`X-Feature-Flag-Poll-Token`ヘッダ)。ユーザーセッションに紐づかない定期ポーリングのため
	// 通常のJWT(RequireAuth)は使えず、backend側にも同じ値を設定する専用の認可方式にしている
	FeatureFlagPollToken string

	// LocalAuthInternalTokenURL はbackendの
	// POST /internal/v1/auth/verify-local-password のURL(CONTRACT.mdセクション16.3)
	LocalAuthVerifyPasswordURL string
	// LocalAuthInternalToken はLocalAuthInternalTokenURLへのリクエストに付与する
	// 共有シークレット(`X-Local-Auth-Internal-Token`ヘッダ)。まだJWTを持たない
	// (ログイン処理そのものである)呼び出しのため、FeatureFlagPollTokenと同じ考え方の
	// 別シークレットを使う。backend側にも同じ値を設定する
	LocalAuthInternalToken string
	// LocalAuthHMACSecret はローカル認証(HMAC版, iss=bff-gin-local-hmac)のJWT署名鍵
	// backend側の authjwt.HMACVerifier に同じ値を設定する(CONTRACT.mdセクション16.4/16.5)
	LocalAuthHMACSecret string

	// CONTRACT.mdセクション22: パスキー(WebAuthn)関連。ローカル認証ユーザーへの
	// 追加の認証手段として、既存のローカルHMACセッション発行経路にそのまま合流させる
	WebauthnRPID          string
	WebauthnRPDisplayName string
	WebauthnRPOrigin      string
	// WebauthnInternalToken はbackendの
	// GET/PATCH /internal/v1/auth/webauthn/credentials/:credential_id への
	// リクエストで付与する共有シークレット(`X-Webauthn-Internal-Token`ヘッダ)。
	// ログイン試行中はまだJWTを持たないため、LocalAuthInternalTokenと同じ考え方。
	// backend側にも同じ値を設定する
	WebauthnInternalToken string

	// AppEnv が "development" の場合、CookieのSecure属性を落として
	// http://localhost での動作確認を可能にする(本番は必ずSecureにすること)
	AppEnv string

	SessionCookieName string
	CSRFCookieName    string

	// LogLevel は "debug"/"info"/"warn"/"error"。training-go/gin(参照元)と同じ設計
	LogLevel string
}

// SlogLevel はLogLevel文字列をslog.Levelへ変換する。不明な値はInfo扱い
func (c *Config) SlogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Load は環境変数からConfigを構築する
//
// 【学習用途としての判断】OIDC_ISSUER_URL/OIDC_CLIENT_ID/OIDC_CLIENT_SECRET/
// OIDC_REDIRECT_URLは、以前は未設定だとエラーで起動を止める「必須環境変数」
// だった。しかし固定のdocker-compose構成・realm-export.jsonに対しては値が
// 一意に決まるため、都度シェルで長いコマンドを打つ負担を減らすことを優先し、
// その構成に対応する既定値を持たせた(本番運用するアプリではこの判断はしない
// 値そのものが秘密情報や環境固有情報になるため)
func Load() (*Config, error) {
	cfg := &Config{
		Port:                   getEnv("PORT", "8080"),
		RedisAddr:              getEnv("REDIS_ADDR", "127.0.0.1:16379"),
		OIDCIssuerURL:          getEnv("OIDC_ISSUER_URL", "http://localhost:8082/realms/training"),
		OIDCClientID:           getEnv("OIDC_CLIENT_ID", "bff-gin"),
		OIDCClientSecret:       getEnv("OIDC_CLIENT_SECRET", "bff-gin-local-dev-secret"),
		OIDCRedirectURL:        getEnv("OIDC_REDIRECT_URL", "http://localhost:8080/api/auth/callback"),
		PostLogoutRedirect:     getEnv("POST_LOGOUT_REDIRECT_URL", "http://localhost:5173/"),
		FrontendBaseURL:        getEnv("FRONTEND_BASE_URL", "http://localhost:5173"),
		BackendRESTBaseURL:     getEnv("BACKEND_REST_BASE_URL", "http://localhost:8090"),
		BackendGRPCAddr:        getEnv("BACKEND_GRPC_ADDR", "localhost:9090"),
		RustRESTBaseURL:        getEnv("BACKEND_RUST_REST_BASE_URL", "http://localhost:8093"),
		RustGRPCAddr:           getEnv("BACKEND_RUST_GRPC_ADDR", "localhost:9093"),
		ScalaHTTP4sRESTBaseURL: getEnv("BACKEND_SCALA_HTTP4S_REST_BASE_URL", "http://localhost:8094"),
		ScalaHTTP4sGRPCAddr:    getEnv("BACKEND_SCALA_HTTP4S_GRPC_ADDR", "localhost:9094"),
		ScalaPekkoRESTBaseURL:  getEnv("BACKEND_SCALA_PEKKO_REST_BASE_URL", "http://localhost:8095"),
		ScalaPekkoGRPCAddr:     getEnv("BACKEND_SCALA_PEKKO_GRPC_ADDR", "localhost:9095"),
		RailsRESTBaseURL:       getEnv("BACKEND_RAILS_REST_BASE_URL", "http://localhost:8096"),
		RailsGRPCAddr:          getEnv("BACKEND_RAILS_GRPC_ADDR", "localhost:9096"),
		JSRESTBaseURL:          getEnv("BACKEND_JS_REST_BASE_URL", "http://localhost:8103"),
		JSGRPCAddr:             getEnv("BACKEND_JS_GRPC_ADDR", "localhost:9097"),
		TSRESTBaseURL:          getEnv("BACKEND_TS_REST_BASE_URL", "http://localhost:8104"),
		TSGRPCAddr:             getEnv("BACKEND_TS_GRPC_ADDR", "localhost:9098"),
		FeatureFlagFilePath:    getEnv("FEATURE_FLAG_FILE_PATH", "internal/featureflag/flags.yaml"),
		FeatureFlagExportURL: getEnv("FEATURE_FLAG_EXPORT_URL",
			getEnv("BACKEND_REST_BASE_URL", "http://localhost:8090")+"/internal/v1/feature-flags/export"),
		FeatureFlagPollToken: getEnv("FEATURE_FLAG_POLL_TOKEN", "local-dev-feature-flag-poll-token"),
		LocalAuthVerifyPasswordURL: getEnv("LOCAL_AUTH_VERIFY_PASSWORD_URL",
			getEnv("BACKEND_REST_BASE_URL", "http://localhost:8090")+"/internal/v1/auth/verify-local-password"),
		LocalAuthInternalToken: getEnv("LOCAL_AUTH_INTERNAL_TOKEN", "local-dev-local-auth-internal-token"),
		LocalAuthHMACSecret:    getEnv("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me"),
		WebauthnRPID:           getEnv("WEBAUTHN_RP_ID", "localhost"),
		WebauthnRPDisplayName:  getEnv("WEBAUTHN_RP_DISPLAY_NAME", "bff-gin"),
		WebauthnRPOrigin:       getEnv("WEBAUTHN_RP_ORIGIN", "http://localhost:5173"),
		WebauthnInternalToken:  getEnv("WEBAUTHN_INTERNAL_TOKEN", "local-dev-webauthn-internal-token"),
		AppEnv:                 getEnv("APP_ENV", "development"),
		SessionCookieName:      getEnv("SESSION_COOKIE_NAME", "session_id"),
		CSRFCookieName:         getEnv("CSRF_COOKIE_NAME", "csrf_token"),
		LogLevel:               getEnv("LOG_LEVEL", "info"),
	}

	redisDB, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil {
		return nil, fmt.Errorf("REDIS_DBの解釈に失敗しました: %w", err)
	}
	cfg.RedisDB = redisDB

	return cfg, nil
}

// IsDevelopment はCookieのSecure属性などdev/prodで挙動を変える箇所から参照する
func (c *Config) IsDevelopment() bool {
	return c.AppEnv == "development"
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
