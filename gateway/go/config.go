// Package main は外部公開APIゲートウェイ(Go製)のエントリポイント
//
// CONTRACT.mdセクション20.7・20.8参照
// 公開ポート:8081を占有し、backend.task-language フラグの評価結果に応じて対応する言語の backend へリバースプロキシする
// 認証(Client Credentials Grant)の検証はここでは行わず、実際に応答する backend 側(RequireExternalClientAuth)に委譲する薄い層
package main

import (
	"os"
	"strconv"
	"time"
)

// Config は環境変数から読み込む設定値
type Config struct {
	// GatewayAddr はこのゲートウェイ自身の待受アドレス
	// 既定":8081"(既存の外部公開APIの公開ポートをそのまま引き継ぐ)
	GatewayAddr string

	// FeatureFlagExportURL はbackendの GET /internal/v1/feature-flags/export のURL
	// bffと全く同じHTTP retrieverの仕組みでbackend.task-languageをポーリングする
	// (admin/go・admin/railsのようにMySQLへ直接繋ぐ第三の経路は増やさない)
	FeatureFlagExportURL string
	// FeatureFlagPollToken はFeatureFlagExportURLへのリクエストに付与する共有シークレット
	// bff/backendと同じ値を設定する
	FeatureFlagPollToken string
	// FeatureFlagPollIntervalSeconds はポーリング間隔(bffの既定値10秒と合わせる)
	FeatureFlagPollIntervalSeconds int

	// GoExternalBaseURL はbackend(Go実装)の外部公開APIの接続先
	// 【CONTRACT.mdセクション20.7で変更】公開ポート:8081をゲートウェイに明け渡したため、
	// backend自身は内部アドレス:8097で待ち受けるようになった
	GoExternalBaseURL string
	// 【全言語に外部公開API実装が完了したため追加】各言語の外部公開API接続先
	RustExternalBaseURL        string
	ScalaHTTP4sExternalBaseURL string
	ScalaPekkoExternalBaseURL  string
	RailsExternalBaseURL       string

	// LogLevel は "debug"/"info"/"warn"/"error"
	LogLevel string

	// AllowedOrigin はこのゲートウェイへブラウザから直接クロスオリジンで
	// アクセスすることを許可するオリジン(CORS)
	//
	// 【テスト監査で発見・追記】Client Credentials Grantのサーバー間クライアントは
	// CORSを必要としない(ブラウザ以外はSame-Origin Policyの対象外)が、
	// swagger-ui(:18080)の「Try it out」機能はブラウザから直接この
	// ゲートウェイ(:8081)へfetchするため、CORSヘッダが無いと
	// (curlでの動作確認では気づけないが)実際のブラウザ上で失敗する。
	// 既定値はdocker-compose.yamlのswagger-uiのポート(:18080)に合わせている
	AllowedOrigin string
}

// Load は環境変数からConfigを組み立てる
// 【学習用途としての判断】既定のdocker-compose構成に対応する値を全項目に持たせている
// (backend/bffのconfig.goと同じ判断、CONTRACT.md参照)
func Load() Config {
	cfg := Config{
		GatewayAddr:                getEnv("GATEWAY_ADDR", ":8081"),
		FeatureFlagExportURL:       getEnv("FEATURE_FLAG_EXPORT_URL", "http://localhost:8090/internal/v1/feature-flags/export"),
		FeatureFlagPollToken:       getEnv("FEATURE_FLAG_POLL_TOKEN", "local-dev-feature-flag-poll-token"),
		GoExternalBaseURL:          getEnv("GATEWAY_GO_EXTERNAL_BASE_URL", "http://localhost:8097"),
		RustExternalBaseURL:        getEnv("GATEWAY_RUST_EXTERNAL_BASE_URL", "http://localhost:8098"),
		ScalaHTTP4sExternalBaseURL: getEnv("GATEWAY_SCALA_HTTP4S_EXTERNAL_BASE_URL", "http://localhost:8099"),
		ScalaPekkoExternalBaseURL:  getEnv("GATEWAY_SCALA_PEKKO_EXTERNAL_BASE_URL", "http://localhost:8100"),
		RailsExternalBaseURL:       getEnv("GATEWAY_RAILS_EXTERNAL_BASE_URL", "http://localhost:8101"),
		LogLevel:                   getEnv("LOG_LEVEL", "info"),
		AllowedOrigin:              getEnv("GATEWAY_ALLOWED_ORIGIN", "http://localhost:18080"),
	}

	pollSeconds, err := strconv.Atoi(getEnv("FEATURE_FLAG_POLL_INTERVAL_SECONDS", "10"))
	if err != nil {
		pollSeconds = 10
	}
	cfg.FeatureFlagPollIntervalSeconds = pollSeconds

	return cfg
}

func (c Config) pollInterval() time.Duration {
	return time.Duration(c.FeatureFlagPollIntervalSeconds) * time.Second
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
