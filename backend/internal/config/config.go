// Package config は環境変数からの設定読み込みを担う
// Rails: config/database.yml + Rails.application.credentials に相当する内容を、
// Goでは12-factor的に環境変数から読む(このプロジェクト全体の方針)
package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config はbackendプロセス全体の設定値
type Config struct {
	// HTTPAddr はREST v1(Gin)の待受アドレス
	// 既定":8090"
	// 【環境構築の実機検証で判明した変更】backend/bffは(KeycloakのAuthorization
	// Code Flowにおけるブラウザ⇔コンテナ間のホスト名不一致を避けるため)ともに
	// コンテナ化せずホスト上でネイティブ実行する設計にしている(docker-compose.yaml 参照)
	// この場合、bffの既定ポート:8080と衝突するため、backendのREST v1は
	// :8090へ変更した(CONTRACT.mdに書いていた:8080はコンテナ化前提の値だった)
	HTTPAddr string
	// GRPCAddr はgRPC v2の待受アドレス
	// 例: ":9090"(CONTRACT.md/bffのBACKEND_GRPC_ADDR既定値と合わせる)
	GRPCAddr string
	// ExternalHTTPAddr はBFF非経由の外部公開API(CONTRACT.mdセクション11)の待受アドレス
	// REST v1(:8090)・gRPC v2(:9090)とは別ポート
	// 【CONTRACT.mdセクション20.7で変更】公開ポート:8081は新設したgatewayが占有するようになったため、
	// この値はgatewayが振り分ける先のGo実装の内部アドレスとなり、:8097へ変更した
	// (:8081は今後gateway/go・gateway/nginxのいずれかが待ち受ける)
	// 既定":8097"
	ExternalHTTPAddr string
	// ExternalAPIClientID はClient Credentials Grantで発行されたトークンのazp
	// (authorized party)クレームと比較する期待値
	// 既定"external-api-client"
	ExternalAPIClientID string
	// ExternalFeatureFlagPath は外部API専用のFeature Flag定義ファイル(flags.yaml)のパス
	// 【CONTRACT.mdセクション13で置き換え済み】実際にはFeatureFlagPollIntervalの
	// MySQL retrieverが使われており、このファイルは参考用に残しているだけで読まれない
	ExternalFeatureFlagPath string

	// FeatureFlagPollToken は GET /internal/v1/feature-flags/export を叩く際に
	// `X-Feature-Flag-Poll-Token` ヘッダで要求する共有シークレット
	// bff 側にも同じ値を設定する(bffの FEATURE_FLAG_POLL_TOKEN と揃える。既定値も同じ)
	FeatureFlagPollToken string
	// FeatureFlagPollInterval はbackend自身のMySQL retrieverがfeature_flagsテーブルを再読込する間隔
	// admin/go・admin/railsからの変更が反映されるまでの遅延になる
	FeatureFlagPollInterval time.Duration

	// DBDSN はMySQLへの接続文字列(go-sql-driver/mysql形式)
	// 既定値はdocker-compose.yamlのmysqlサービス(127.0.0.1:13306、root/パスワードなし)
	// にネイティブ実行のbackendから接続する値(学習用途のためDSNをコードに埋め込むことを許容している
	// 本番相当の秘密情報ではない)
	DBDSN string

	// KeycloakIssuer は例: http://localhost:8082/realms/training
	// JWTの `iss` クレーム検証、およびJWKS URL導出に使う
	// 既定値はdocker-compose.yamlのKC_HOSTNAME=localhost/KC_HOSTNAME_PORT=8082設定に
	// 対応する(ブラウザが到達するURLとissuerを一致させるための固定値)
	KeycloakIssuer string
	// KeycloakJWKSURL は明示指定が無ければ `${KeycloakIssuer}/protocol/openid-connect/certs` を使う
	KeycloakJWKSURL string
	// ExpectedAudience はJWTの `aud` クレームに含まれるべき値
	// 【実機検証で判明】bff-ginクライアントの認可コードフローで発行される
	// Access Tokenには、Keycloakの既定動作では aud クレームが一切含まれない
	// (client_credentials Grant(external-api-client)では aud=account が付くが、認可コードフローでは付かない)
	// そのためrealm-export.jsonの bff-gin /
	// external-api-client 両クライアントに、固定値 "backend" を aud として注入する
	// Audience Protocol Mapper(oidc-audience-mapper, included.custom.audience)を追加した
	// この値と一致させる
	ExpectedAudience string

	// LocalAuthInternalToken は POST /internal/v1/auth/verify-local-password への
	// リクエストで要求する共有シークレット(`X-Local-Auth-Internal-Token`ヘッダ)
	// bff側の LOCAL_AUTH_INTERNAL_TOKEN と同じ値にする(CONTRACT.mdセクション16.3)
	LocalAuthInternalToken string
	// LocalHMACSecret はローカル認証(HMAC版、iss=authjwt.LocalHMACIssuer)のJWT署名鍵
	// bff側の LOCAL_AUTH_HMAC_SECRET と同じ値にする(CONTRACT.mdセクション16.4/16.5)
	LocalHMACSecret string
	// LocalRSAJWKSURL はローカル認証(RSA版、iss=authjwt.LocalRSAIssuer)のJWKS取得先
	// bffが公開する GET /.well-known/jwks.json を指す(bffのポート、backendのポートではない点に注意)
	LocalRSAJWKSURL string

	// AdminInternalToken は POST/DELETE /internal/v1/admin/users 系への
	// リクエストで要求する共有シークレット(`X-Admin-Internal-Token`ヘッダ)
	// admin/go・admin/railsはユーザーのJWTを持たない(Basic Authで自分自身を守るだけの
	// 別アプリ)ため、FeatureFlagPollToken/LocalAuthInternalTokenと同じ「共有シークレット
	// ヘッダ」方式で認可する(CONTRACT.mdセクション17.3)
	// admin/go・admin/rails側にも同じ値を設定する
	AdminInternalToken string

	// WebauthnInternalToken は GET/PATCH /internal/v1/auth/webauthn/credentials/:credential_id への
	// リクエストで要求する共有シークレット(`X-Webauthn-Internal-Token`ヘッダ)
	// パスキーでのログイン試行中はまだJWTが存在しない(ログイン処理そのもの)ため、
	// LocalAuthInternalTokenと同じ考え方の別の共有シークレットで認可する(CONTRACT.mdセクション22.4)
	// bff側にも同じ値を設定する
	WebauthnInternalToken string

	// LogLevel は "debug"/"info"/"warn"/"error"
	// LOG_LEVEL=debug で起動すると、GORMが発行したSQLもログに出るようになる
	// (cmd/server/main.go, internal/db/logger.go参照)
	LogLevel string
}

// SlogLevel はLogLevel文字列をslog.Levelへ変換する
// 不明な値はInfo扱い
// training-go/gin: internal/config/config.go の同名メソッドに倣う
func (c Config) SlogLevel() slog.Level {
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

// Load は環境変数からConfigを組み立てる
//
// Rails対比: Rails.application.credentials.dig(...) のようにネストしたYAMLを
// 読むのではなく、Goでは「フラットな環境変数を1箇所で読み切る」構成にすることで、
// どの設定がどこで使われるかをこの1ファイルで見渡せるようにしている
//
// 【学習用途としての判断】DB_DSN/KEYCLOAK_ISSUER/EXPECTED_AUDIENCEは、以前は
// 未設定だとエラーで起動を止める「必須環境変数」だった
// しかし固定のdocker-compose 構成に対しては値が一意に決まるため、都度シェルで長いコマンドを打つ負担を減らすことを優先し、
// その構成に対応する既定値を持たせた(本番運用するアプリではこの判断はしない
// 値そのものが秘密情報や環境固有情報になるため)
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:                getEnvDefault("HTTP_ADDR", ":8090"),
		GRPCAddr:                getEnvDefault("GRPC_ADDR", ":9090"),
		ExternalHTTPAddr:        getEnvDefault("EXTERNAL_HTTP_ADDR", ":8097"),
		ExternalAPIClientID:     getEnvDefault("EXTERNAL_API_CLIENT_ID", "external-api-client"),
		ExternalFeatureFlagPath: getEnvDefault("EXTERNAL_FEATURE_FLAG_PATH", "internal/featureflag/flags.yaml"),
		FeatureFlagPollToken:    getEnvDefault("FEATURE_FLAG_POLL_TOKEN", "local-dev-feature-flag-poll-token"),
		DBDSN:                   getEnvDefault("DB_DSN", "root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true"),
		KeycloakIssuer:          getEnvDefault("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training"),
		KeycloakJWKSURL:         os.Getenv("KEYCLOAK_JWKS_URL"),
		ExpectedAudience:        getEnvDefault("EXPECTED_AUDIENCE", "backend"),
		LocalAuthInternalToken:  getEnvDefault("LOCAL_AUTH_INTERNAL_TOKEN", "local-dev-local-auth-internal-token"),
		LocalHMACSecret:         getEnvDefault("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me"),
		LocalRSAJWKSURL:         getEnvDefault("LOCAL_AUTH_RSA_JWKS_URL", "http://localhost:8080/.well-known/jwks.json"),
		AdminInternalToken:      getEnvDefault("ADMIN_INTERNAL_TOKEN", "local-dev-admin-internal-token"),
		WebauthnInternalToken:   getEnvDefault("WEBAUTHN_INTERNAL_TOKEN", "local-dev-webauthn-internal-token"),
		LogLevel:                getEnvDefault("LOG_LEVEL", "info"),
	}

	if cfg.KeycloakJWKSURL == "" {
		cfg.KeycloakJWKSURL = cfg.KeycloakIssuer + "/protocol/openid-connect/certs"
	}

	pollSeconds, err := strconv.Atoi(getEnvDefault("FEATURE_FLAG_POLL_INTERVAL_SECONDS", "10"))
	if err != nil {
		pollSeconds = 10
	}
	cfg.FeatureFlagPollInterval = time.Duration(pollSeconds) * time.Second

	return cfg, nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
