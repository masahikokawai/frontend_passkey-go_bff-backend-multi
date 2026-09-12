package main

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("GATEWAY_ADDR", "")
	t.Setenv("FEATURE_FLAG_EXPORT_URL", "")
	t.Setenv("FEATURE_FLAG_POLL_TOKEN", "")
	t.Setenv("GATEWAY_GO_EXTERNAL_BASE_URL", "")
	t.Setenv("FEATURE_FLAG_POLL_INTERVAL_SECONDS", "")
	t.Setenv("LOG_LEVEL", "")

	cfg := Load()

	if cfg.GatewayAddr != ":8081" {
		t.Errorf("GatewayAddr = %q, want :8081", cfg.GatewayAddr)
	}
	if cfg.FeatureFlagExportURL != "http://localhost:8090/internal/v1/feature-flags/export" {
		t.Errorf("FeatureFlagExportURL = %q, want backend :8090 export URL", cfg.FeatureFlagExportURL)
	}
	if cfg.FeatureFlagPollToken != "local-dev-feature-flag-poll-token" {
		t.Errorf("FeatureFlagPollToken = %q, want the shared local-dev token", cfg.FeatureFlagPollToken)
	}
	if cfg.GoExternalBaseURL != "http://localhost:8097" {
		t.Errorf("GoExternalBaseURL = %q, want backendの新しい外部公開APIアドレス:8097", cfg.GoExternalBaseURL)
	}
	if cfg.FeatureFlagPollIntervalSeconds != 10 {
		t.Errorf("FeatureFlagPollIntervalSeconds = %d, want 10", cfg.FeatureFlagPollIntervalSeconds)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("GATEWAY_ADDR", ":9999")
	t.Setenv("GATEWAY_GO_EXTERNAL_BASE_URL", "http://localhost:12345")

	cfg := Load()

	if cfg.GatewayAddr != ":9999" {
		t.Errorf("GatewayAddr = %q, want :9999(env override)", cfg.GatewayAddr)
	}
	if cfg.GoExternalBaseURL != "http://localhost:12345" {
		t.Errorf("GoExternalBaseURL = %q, want http://localhost:12345(env override)", cfg.GoExternalBaseURL)
	}
}
