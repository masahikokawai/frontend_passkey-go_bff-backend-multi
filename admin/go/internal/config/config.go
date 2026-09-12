// Package config は環境変数からの設定読み込みを担う
// backend/bffと同じ12-factor的な方針(フラットな環境変数を1箇所で読み切る)を踏襲する
package config

import "os"

// Config はこのアプリ全体の設定値
type Config struct {
	// Port はHTTPの待受ポート。既定":8091"(CONTRACT.mdセクション13)
	Port string
	// DBDSN はMySQLへの接続文字列(go-sql-driver/mysql形式)
	// 既定値はdocker-compose.yamlのmysqlサービスに対応する
	// (root@tcp(127.0.0.1:13306)/bff_gin_development、パスワードなし、学習用途のため)
	DBDSN string
	// BasicAuthUser/BasicAuthPassword はHTTP Basic Authの資格情報
	// このアプリはOIDCを導入せず、学習用の簡易認証として割り切っている
	// (CONTRACT.mdセクション13: CRUD実装比較が主眼のため認証方式はあえて簡略化)
	BasicAuthUser     string
	BasicAuthPassword string
	// BackendInternalBaseURL はbackendの内部APIの接続先
	// (CONTRACT.mdセクション17.3: ユーザー管理はFeature Flagと異なりDB直結ではなく、
	// backendの /internal/v1/admin/users をHTTP経由で叩く設計にしている。
	// 理由は「最後の管理者を降格/削除できない」といった業務ルールをbackend1箇所に
	// 集約し、admin/go・admin/railsでの重複実装によるズレを防ぐため)
	BackendInternalBaseURL string
	// AdminInternalToken は上記APIへのリクエストに付与する共有シークレット
	// (`X-Admin-Internal-Token`ヘッダ。backend側の既定値と一致させること)
	AdminInternalToken string
}

// Load は環境変数からConfigを組み立てる
func Load() Config {
	return Config{
		Port:                   getEnv("PORT", "8091"),
		DBDSN:                  getEnv("DB_DSN", "root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true"),
		BasicAuthUser:          getEnv("ADMIN_BASIC_AUTH_USER", "admin"),
		BasicAuthPassword:      getEnv("ADMIN_BASIC_AUTH_PASSWORD", "password"),
		BackendInternalBaseURL: getEnv("BACKEND_INTERNAL_BASE_URL", "http://localhost:8090"),
		AdminInternalToken:     getEnv("ADMIN_INTERNAL_TOKEN", "local-dev-admin-internal-token"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
