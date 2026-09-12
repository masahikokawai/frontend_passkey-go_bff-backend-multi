package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := loadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	targets := map[string]string{
		// gateway/goと同じ: 5言語すべてが外部公開APIを実装済み
		"go":           cfg.GoExternalBaseURL,
		"rust":         cfg.RustExternalBaseURL,
		"scala-http4s": cfg.ScalaHTTP4sExternalBaseURL,
		"scala-pekko":  cfg.ScalaPekkoExternalBaseURL,
		"rails":        cfg.RailsExternalBaseURL,
	}

	logger.Info("nginx製ゲートウェイのサイドカーを起動します",
		"export_url", cfg.FeatureFlagExportURL, "upstream_conf", cfg.UpstreamConfPath, "poll_interval_seconds", cfg.PollIntervalSeconds)

	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	// 起動直後にも1回実行する(ポーリング間隔ぶん待たされないように)
	tick(cfg, targets, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info("サイドカーを停止します")
			return
		case <-ticker.C:
			tick(cfg, targets, logger)
		}
	}
}

func tick(cfg sidecarConfig, targets map[string]string, logger *slog.Logger) {
	requested, err := fetchLanguage(cfg.FeatureFlagExportURL, cfg.FeatureFlagPollToken)
	if err != nil {
		logger.Warn("backend.task-languageのポーリングに失敗しました", "error", err)
		return
	}

	resolved, base, fellBack := resolveTarget(requested, targets)
	if fellBack {
		logger.Warn("backend.task-languageが未実装の言語を指しているためgoへフォールバック",
			"requested_language", requested, "fallback_language", resolved)
	}

	desired, err := renderUpstreamConf(base)
	if err != nil {
		logger.Error("upstream.confのレンダリングに失敗しました", "error", err)
		return
	}

	current, err := os.ReadFile(cfg.UpstreamConfPath)
	if err == nil && string(current) == desired {
		// 変化なし
		// ファイル書き換え・reloadは行わない
		return
	}

	if err := os.WriteFile(cfg.UpstreamConfPath, []byte(desired), 0o644); err != nil {
		logger.Error("upstream.confの書き換えに失敗しました", "error", err)
		return
	}
	logger.Info("upstream.confを書き換えました", "resolved_language", resolved, "base_url", base)

	if err := reloadNginx(cfg.NginxPrefixDir); err != nil {
		logger.Error("nginx -s reloadに失敗しました", "error", err)
		return
	}
	logger.Info("nginxをreloadしました")
}

func fetchLanguage(exportURL, pollToken string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, exportURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Feature-Flag-Poll-Token", pollToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return parseLanguage(body)
}

func reloadNginx(prefixDir string) error {
	cmd := exec.Command("nginx", "-p", prefixDir, "-c", "nginx.conf", "-s", "reload")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type sidecarConfig struct {
	FeatureFlagExportURL       string
	FeatureFlagPollToken       string
	PollIntervalSeconds        int
	GoExternalBaseURL          string
	RustExternalBaseURL        string
	ScalaHTTP4sExternalBaseURL string
	ScalaPekkoExternalBaseURL  string
	RailsExternalBaseURL       string
	UpstreamConfPath           string
	NginxPrefixDir             string
}

func loadConfig() sidecarConfig {
	pollSeconds, err := strconv.Atoi(getEnv("FEATURE_FLAG_POLL_INTERVAL_SECONDS", "10"))
	if err != nil {
		pollSeconds = 10
	}
	return sidecarConfig{
		FeatureFlagExportURL:       getEnv("FEATURE_FLAG_EXPORT_URL", "http://localhost:8090/internal/v1/feature-flags/export"),
		FeatureFlagPollToken:       getEnv("FEATURE_FLAG_POLL_TOKEN", "local-dev-feature-flag-poll-token"),
		PollIntervalSeconds:        pollSeconds,
		GoExternalBaseURL:          getEnv("GATEWAY_GO_EXTERNAL_BASE_URL", "http://localhost:8097"),
		RustExternalBaseURL:        getEnv("GATEWAY_RUST_EXTERNAL_BASE_URL", "http://localhost:8098"),
		ScalaHTTP4sExternalBaseURL: getEnv("GATEWAY_SCALA_HTTP4S_EXTERNAL_BASE_URL", "http://localhost:8099"),
		ScalaPekkoExternalBaseURL:  getEnv("GATEWAY_SCALA_PEKKO_EXTERNAL_BASE_URL", "http://localhost:8100"),
		RailsExternalBaseURL:       getEnv("GATEWAY_RAILS_EXTERNAL_BASE_URL", "http://localhost:8101"),
		UpstreamConfPath:           getEnv("UPSTREAM_CONF_PATH", "../upstream.conf"),
		NginxPrefixDir:             getEnv("NGINX_PREFIX_DIR", ".."),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
