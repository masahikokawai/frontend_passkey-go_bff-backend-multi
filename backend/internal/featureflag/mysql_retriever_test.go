package featureflag

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// BuildFlagConfigJSONが、GO Feature Flagが期待するJSON形状
// ({variations, defaultRule: {variation}, disable})を正しく組み立てることを確認する
// このJSONはbackend自身のMySQL retrieverと、bff向けのexportエンドポイント
// (internal/handler/v1/feature_flag_export.go)の両方から使われる共通ロジック
//
// 【CONTRACT.mdセクション19で拡張】Variationsがmap[string]bool決め打ちからmap[string]anyへ
// 一般化されたのに伴いテーブル駆動へ書き換えたが、既存2フラグ(Variations未設定→
// {"on":true,"off":false}にフォールバックする経路)の期待値は変更前と意味的に同一のまま
//
// 新たに、DBのvariations列を明示的に持つ多値(文字列3値)フラグのケースを追加した
func TestBuildFlagConfigJSON(t *testing.T) {
	tests := []struct {
		name  string
		flags []model.FeatureFlag
		want  map[string]flagConfig
	}{
		{
			name: "Variations未設定の既存フラグはboolean(on/off)へフォールバックする",
			flags: []model.FeatureFlag{
				{FlagKey: "frontend.tasks-ts-rewrite", DefaultVariation: "off", Enabled: true},
				{FlagKey: "bff.tasks-backend-v2", DefaultVariation: "on", Enabled: false},
			},
			want: map[string]flagConfig{
				"frontend.tasks-ts-rewrite": {
					Variations:  map[string]any{"on": true, "off": false},
					DefaultRule: defaultRule{Variation: "off"},
					Disable:     false, // Enabled=trueなのでdisable=false
				},
				"bff.tasks-backend-v2": {
					Variations:  map[string]any{"on": true, "off": false},
					DefaultRule: defaultRule{Variation: "on"},
					Disable:     true, // Enabled=falseなのでdisable=true(反転)
				},
			},
		},
		{
			name: "Variationsが明示的に設定されたboolean値もそのまま使われる(マイグレーションでバックフィルされた既存フラグの実際の形)",
			flags: []model.FeatureFlag{
				{FlagKey: "backend.external-tasks-pagination-v2", DefaultVariation: "off", Enabled: true, Variations: `{"on":true,"off":false}`},
			},
			want: map[string]flagConfig{
				"backend.external-tasks-pagination-v2": {
					Variations:  map[string]any{"on": true, "off": false},
					DefaultRule: defaultRule{Variation: "off"},
					Disable:     false,
				},
			},
		},
		{
			name: "Variationsが文字列3値(多値フラグ)の場合もそのまま使われる(CONTRACT.mdセクション19、frontend.task-create-ux)",
			flags: []model.FeatureFlag{
				{
					FlagKey:          "frontend.task-create-ux",
					DefaultVariation: "inline",
					Enabled:          true,
					Variations:       `{"inline":"inline","modal":"modal","page":"page"}`,
				},
			},
			want: map[string]flagConfig{
				"frontend.task-create-ux": {
					Variations:  map[string]any{"inline": "inline", "modal": "modal", "page": "page"},
					DefaultRule: defaultRule{Variation: "inline"},
					Disable:     false,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := BuildFlagConfigJSON(tt.flags)
			if err != nil {
				t.Fatalf("BuildFlagConfigJSON失敗: %v", err)
			}

			var got map[string]flagConfig
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("生成されたJSONのパースに失敗: %v", err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("BuildFlagConfigJSONの結果が一致しない(-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildFlagConfigJSON_empty(t *testing.T) {
	body, err := BuildFlagConfigJSON(nil)
	if err != nil {
		t.Fatalf("空のフラグ一覧でエラー: %v", err)
	}
	if string(body) != "{}" {
		t.Errorf("body = %s, want {}", body)
	}
}

// TestBuildFlagConfigJSON_invalidVariationsJSON はvariations列に壊れたJSONが
// 入っていた場合、黙って無視せずエラーを返すことを確認する(サイレントな設定ミスを防ぐ)
func TestBuildFlagConfigJSON_invalidVariationsJSON(t *testing.T) {
	flags := []model.FeatureFlag{
		{FlagKey: "broken", DefaultVariation: "on", Enabled: true, Variations: `{not valid json`},
	}
	if _, err := BuildFlagConfigJSON(flags); err == nil {
		t.Error("BuildFlagConfigJSON() error = nil, 壊れたVariations JSONに対してエラーを期待した")
	}
}

// TestBuildFlagConfigJSON_emptyVariationsObject は variations 列が構文的には正しいがキーが0個の空オブジェクト({})の場合の挙動を確認する
//
// これは「壊れたJSON」とは違い
// json.Unmarshal 自体は成功するため、invalidVariationsJSON のケースでは捕捉できない
// (admin/go の VariationOptions/IsBooleanToggle が同じ「空オブジェクト」を後方互換のフォールバック対象として明示的に扱っているのと対になる観点)
func TestBuildFlagConfigJSON_emptyVariationsObject(t *testing.T) {
	flags := []model.FeatureFlag{
		{FlagKey: "empty-variations", DefaultVariation: "on", Enabled: true, Variations: `{}`},
	}
	body, err := BuildFlagConfigJSON(flags)
	if err != nil {
		t.Fatalf("BuildFlagConfigJSON()でエラー: %v", err)
	}

	var got map[string]flagConfig
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("生成されたJSONのパースに失敗: %v", err)
	}
	// 空オブジェクトは「フォールバックせずそのまま空」として出力される(defaultBooleanVariationsへの
	// フォールバックはVariationsが空文字列/未設定の場合だけで、"{}"は明示的な値として尊重する)
	if diff := cmp.Diff(map[string]any{}, got["empty-variations"].Variations); diff != "" {
		t.Errorf("Variations mismatch (-want +got):\n%s", diff)
	}
}
