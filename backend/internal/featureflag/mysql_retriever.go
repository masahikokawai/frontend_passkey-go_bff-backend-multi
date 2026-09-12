package featureflag

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// mysqlRetriever はGO Feature Flagのretriever.Retrieverインタフェース
// (Retrieve(ctx) ([]byte, error)を持つだけの薄いインタフェース)を、
// MySQLのfeature_flagsテーブルに対して実装したもの
//
// 【設計判断・CONTRACT.mdセクション13参照】以前はファイル(flags.yaml)を読むだけの
// fileretriever.Retrieverを使っていたが、admin画面からの変更を再デプロイ無しで反映できるようにするため、MySQLを正本にする
// backendは元々GORM接続を持つため、
// bff側(HTTP retrieverでbackendのexportエンドポイントをポーリングする)とは違い、
// HTTPを経由せず直接クエリする
type mysqlRetriever struct {
	repo *repository.FeatureFlag
}

// flagConfig はGO Feature Flagが読み込む1フラグ分のスキーマ(JSON)
// https://gofeatureflag.org のflag設定フォーマットに合わせる
//
// 【CONTRACT.mdセクション19で一般化】Variationsは以前 map[string]bool 決め打ちだったが、
// 「inline/modal/page」のような文字列3値のフラグにも対応できるよう map[string]any にした
//
// encoding/jsonはbool/string/数値いずれもanyへ問題無く(un)marshalできるため、boolean専用だった頃の既存フラグの出力とも互換性がある
type flagConfig struct {
	Variations  map[string]any `json:"variations"`
	DefaultRule defaultRule    `json:"defaultRule"`
	Disable     bool           `json:"disable"`
}

type defaultRule struct {
	Variation string `json:"variation"`
}

// defaultBooleanVariations は model.FeatureFlag.Variations が空
// (古い行、または何らかの理由で未設定)の場合に使うフォールバック
//
// 一般化前にハードコードされていた値と完全に同じであり、既存フラグの出力結果を変えないための後方互換の要
var defaultBooleanVariations = map[string]any{"on": true, "off": false}

// BuildFlagConfigJSON はfeature_flagsテーブルの内容をGO Feature Flagが期待する JSON形式へ変換する
// bffのexportエンドポイント(GET /internal/v1/feature-flags/export)
// もこれと全く同じ関数を使うことで、backend自身のMySQL retrieverとbff向けの
// HTTPレスポンスの形状を1箇所で揃えている
//
// 【CONTRACT.mdセクション19で一般化】以前は全フラグ一律で`{"on":true,"off":false}`と
// 決め打ちしていたが、DBの`variations`列(JSON文字列)をそのまま使うよう変更した
//
// これによりフラグごとにboolean(on/off)・文字列(3値以上)いずれのvariationsも表現できる
func BuildFlagConfigJSON(flags []model.FeatureFlag) ([]byte, error) {
	out := make(map[string]flagConfig, len(flags))
	for _, f := range flags {
		variations := defaultBooleanVariations
		if f.Variations != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(f.Variations), &parsed); err != nil {
				return nil, fmt.Errorf("feature_flags.variations(flag_key=%s)のJSONパースに失敗しました: %w", f.FlagKey, err)
			}
			variations = parsed
		}
		out[f.FlagKey] = flagConfig{
			Variations:  variations,
			DefaultRule: defaultRule{Variation: f.DefaultVariation},
			Disable:     !f.Enabled,
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("feature flag設定のJSON変換に失敗しました: %w", err)
	}
	return b, nil
}

// Retrieve はGO Feature Flagの PollingInterval のたびに呼ばれ、MySQLから
// 最新のフラグ設定を読み直す
func (r *mysqlRetriever) Retrieve(ctx context.Context) ([]byte, error) {
	flags, err := r.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("MySQL retrieverでのfeature_flags取得に失敗しました: %w", err)
	}
	return BuildFlagConfigJSON(flags)
}
