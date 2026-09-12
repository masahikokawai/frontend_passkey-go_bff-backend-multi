package main

import "testing"

func TestParseLanguage(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "backend.task-languageのdefaultRule.variationを取り出せる",
			body: `{"backend.task-language":{"variations":{"go":"go","rust":"rust"},"defaultRule":{"variation":"rust"},"disable":false}}`,
			want: "rust",
		},
		{
			name: "他のフラグが混ざっていても正しく取り出せる",
			body: `{"frontend.tasks-ts-rewrite":{"variations":{"on":true,"off":false},"defaultRule":{"variation":"off"},"disable":false},` +
				`"backend.task-language":{"variations":{"go":"go"},"defaultRule":{"variation":"go"},"disable":false}}`,
			want: "go",
		},
		{
			name:    "backend.task-languageが無ければエラー",
			body:    `{"frontend.tasks-ts-rewrite":{"defaultRule":{"variation":"off"}}}`,
			wantErr: true,
		},
		{
			name:    "不正なJSONはエラー",
			body:    `not json`,
			wantErr: true,
		},
		{
			name:    "variationが空文字はエラー",
			body:    `{"backend.task-language":{"defaultRule":{"variation":""}}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLanguage([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("got = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTarget(t *testing.T) {
	targets := map[string]string{"go": "http://localhost:8097"}

	tests := []struct {
		name         string
		requested    string
		wantResolved string
		wantFellBack bool
	}{
		{name: "goはそのまま", requested: "go", wantResolved: "go", wantFellBack: false},
		{name: "rustはgoへフォールバック", requested: "rust", wantResolved: "go", wantFellBack: true},
		{name: "scala-http4sはgoへフォールバック", requested: "scala-http4s", wantResolved: "go", wantFellBack: true},
		{name: "scala-pekkoはgoへフォールバック", requested: "scala-pekko", wantResolved: "go", wantFellBack: true},
		{name: "railsはgoへフォールバック", requested: "rails", wantResolved: "go", wantFellBack: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, base, fellBack := resolveTarget(tt.requested, targets)
			if resolved != tt.wantResolved {
				t.Errorf("resolved = %q, want %q", resolved, tt.wantResolved)
			}
			if base != targets["go"] {
				t.Errorf("baseURL = %q, want %q", base, targets["go"])
			}
			if fellBack != tt.wantFellBack {
				t.Errorf("fellBack = %v, want %v", fellBack, tt.wantFellBack)
			}
		})
	}
}

func TestRenderUpstreamConf(t *testing.T) {
	got, err := renderUpstreamConf("http://127.0.0.1:8097")
	if err != nil {
		t.Fatalf("renderUpstreamConf() error = %v", err)
	}
	want := "# このファイルはsidecarが自動的に書き換える(CONTRACT.mdセクション20.8)\n" +
		"# 手動編集してもsidecarの次回ポーリングで上書きされる\n" +
		"upstream backend_external {\n    server 127.0.0.1:8097;\n}\n"
	if got != want {
		t.Errorf("renderUpstreamConf() = %q, want %q", got, want)
	}
}

func TestRenderUpstreamConf_InvalidURL(t *testing.T) {
	if _, err := renderUpstreamConf("://not-a-url"); err == nil {
		t.Error("不正なURLでもエラーにならなかった")
	}
	if _, err := renderUpstreamConf("just-a-path-no-host"); err == nil {
		t.Error("ホストが無いURLでもエラーにならなかった")
	}
}
