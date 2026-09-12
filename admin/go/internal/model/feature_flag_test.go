package model

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// admin/rails側のspec/models/feature_flag_spec.rb「多値フラグ(variationsカラム)」と
// 対になるテスト(あちらのコメントが本テストの存在を前提にしていたが、実際には
// admin/go/internal/modelパッケージにテストファイルが1つも無かったため新設した)
//
// VariationOptions/IsBooleanToggle/parseVariationsは、nilや不正JSON等の分岐を複数持つ
// 純粋なロジックだが、これまでDB依存の統合テスト(service層)経由でしか間接的に
// 検証されていなかった

func strPtrForTest(s string) *string { return &s }

func TestFeatureFlag_VariationOptions(t *testing.T) {
	tests := []struct {
		name       string
		variations *string
		want       []string
	}{
		{
			name:       "Variationsがnil(未設定の古い行)は既存フラグ相当の[off,on]にフォールバックする",
			variations: nil,
			want:       []string{"off", "on"},
		},
		{
			name:       "Variationsが空文字列も同様にフォールバックする",
			variations: strPtrForTest(""),
			want:       []string{"off", "on"},
		},
		{
			name:       "Variationsが壊れたJSONの場合もフォールバックする(パニックしない)",
			variations: strPtrForTest(`{not valid json`),
			want:       []string{"off", "on"},
		},
		{
			name:       "Variationsが空オブジェクト{}の場合もフォールバックする(キー0個)",
			variations: strPtrForTest(`{}`),
			want:       []string{"off", "on"},
		},
		{
			name:       "Variationsがboolean 2値の場合はそのキーをソート済みで返す",
			variations: strPtrForTest(`{"on":true,"off":false}`),
			want:       []string{"off", "on"},
		},
		{
			name:       "Variationsが文字列3値の場合はソート済みで3つとも返す",
			variations: strPtrForTest(`{"inline":"inline","modal":"modal","page":"page"}`),
			want:       []string{"inline", "modal", "page"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := FeatureFlag{Variations: tt.variations}
			got := f.VariationOptions()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("VariationOptions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFeatureFlag_IsBooleanToggle(t *testing.T) {
	tests := []struct {
		name       string
		variations *string
		want       bool
	}{
		{
			name:       "Variationsがnil(未設定の古い行)は後方互換でtrue扱い",
			variations: nil,
			want:       true,
		},
		{
			name:       "Variationsが空文字列も同様にtrue扱い",
			variations: strPtrForTest(""),
			want:       true,
		},
		{
			name:       "on=true, off=falseの正しい2値ならtrue",
			variations: strPtrForTest(`{"on":true,"off":false}`),
			want:       true,
		},
		{
			name:       "3値フラグはfalse",
			variations: strPtrForTest(`{"inline":"inline","modal":"modal","page":"page"}`),
			want:       false,
		},
		{
			name:       "キーが2つでもon/off以外の名前ならfalse",
			variations: strPtrForTest(`{"yes":true,"no":false}`),
			want:       false,
		},
		{
			name:       "on/offは揃っているが値が反転(on=false,off=true)しているならfalse",
			variations: strPtrForTest(`{"on":false,"off":true}`),
			want:       false,
		},
		{
			name:       "on/offの値がbooleanではなく文字列ならfalse(型不一致)",
			variations: strPtrForTest(`{"on":"true","off":"false"}`),
			want:       false,
		},
		{
			name:       "キーが1つ(onのみ)ならfalse",
			variations: strPtrForTest(`{"on":true}`),
			want:       false,
		},
		{
			name:       "壊れたJSONはフォールバックしてtrue扱い(パニックしない)",
			variations: strPtrForTest(`{not valid json`),
			want:       true,
		},
		{
			name:       "空オブジェクト{}もフォールバックしてtrue扱い",
			variations: strPtrForTest(`{}`),
			want:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := FeatureFlag{Variations: tt.variations}
			if got := f.IsBooleanToggle(); got != tt.want {
				t.Errorf("IsBooleanToggle() = %v, want %v", got, tt.want)
			}
		})
	}
}
