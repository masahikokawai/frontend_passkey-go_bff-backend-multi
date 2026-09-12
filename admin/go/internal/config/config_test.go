package config

import "testing"

// Basic Auth資格情報が既定値に固定されず、環境変数(ADMIN_BASIC_AUTH_USER/PASSWORD)を
// 設定した場合に正しくその値へ差し替わることを確認する
// handler層のテスト(feature_flag_test.go)はミドルウェアへ固定の資格情報を直接渡して
// いるだけで、config.Load()自体が環境変数を正しく読むかは検証されていなかった
func TestLoad_BasicAuthCredentials(t *testing.T) {
	tests := []struct {
		name         string
		envUser      string
		envPassword  string
		wantUser     string
		wantPassword string
	}{
		{
			name:         "環境変数未設定なら既定値",
			wantUser:     "admin",
			wantPassword: "password",
		},
		{
			name:         "環境変数を設定するとその値が使われる",
			envUser:      "custom-admin",
			envPassword:  "custom-secret",
			wantUser:     "custom-admin",
			wantPassword: "custom-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envUser != "" {
				t.Setenv("ADMIN_BASIC_AUTH_USER", tt.envUser)
			}
			if tt.envPassword != "" {
				t.Setenv("ADMIN_BASIC_AUTH_PASSWORD", tt.envPassword)
			}

			cfg := Load()

			if cfg.BasicAuthUser != tt.wantUser {
				t.Errorf("BasicAuthUser = %q, want %q", cfg.BasicAuthUser, tt.wantUser)
			}
			if cfg.BasicAuthPassword != tt.wantPassword {
				t.Errorf("BasicAuthPassword = %q, want %q", cfg.BasicAuthPassword, tt.wantPassword)
			}
		})
	}
}
