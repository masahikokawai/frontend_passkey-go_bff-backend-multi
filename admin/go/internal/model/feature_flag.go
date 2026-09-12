// Package model はfeature_flags/feature_flag_audit_logsテーブルの写し
//
// 重要: このテーブル自体はこのアプリでは作成しない
// 正本は training-go/bff-gin/backend/migrations/ のgolang-migrateが管理しており、
// このアプリ(admin/go)はREADME記載の手順で先にbackend側のマイグレーションを
// 実行済みの前提で、既存テーブルへ接続するだけの内部管理ツールという位置づけ
// (CONTRACT.mdセクション13参照)
package model

import (
	"encoding/json"
	"sort"
	"time"
)

// FeatureFlag は1件のFeature Flag設定
type FeatureFlag struct {
	ID uint64 `gorm:"column:id;primaryKey"`
	// FlagKey はGO Feature Flagが評価に使うキー(例: "frontend.tasks-ts-rewrite")
	FlagKey string `gorm:"column:flag_key"`
	// Description はnull許容(admin画面での説明用メモ)なので*stringで受ける
	Description *string `gorm:"column:description"`
	// DefaultVariation は Variations のキーのいずれか(自由文字列)
	// 以前は"on"/"off"の2値決め打ちだったが、CONTRACT.mdセクション19でVariationsを
	// 導入したことにより、Variationsで定義された任意のキーを取り得るようになった
	DefaultVariation string `gorm:"column:default_variation"`
	// Variations はGO Feature Flagが評価に使う変数の集合をJSON文字列のまま保持する
	// (例: `{"on":true,"off":false}` や `{"inline":"inline","modal":"modal","page":"page"}`)
	//
	// NULL許容カラムのため*stringで受ける(実機検証で判明: DBのvariations列はJSON型で、
	// 空文字列""をそのままINSERTするとMySQLが"Invalid JSON text: The document is empty."で拒否する
	//
	// string(ゼロ値""相当)ではなく*string(ゼロ値nil)にすることで、
	// Variationsを指定しない既存のテストコード等がGORM経由でSQLのNULLを送るようにし、素直に成功するようにしている)
	//
	// NULL/空の場合は従来のON/OFFトグルへフォールバックする
	// (古い行・移行前データとの後方互換、VariationOptions/IsBooleanToggle参照)
	Variations *string   `gorm:"column:variations"`
	Enabled    bool      `gorm:"column:enabled"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (FeatureFlag) TableName() string {
	return "feature_flags"
}

// VariationOptions はVariationsをパースし、キー一覧をソート済みで返す
// パース不能・空の場合は既存3フラグ相当の["off","on"]にフォールバックする
// (このアプリ自体は新旧どちらの行が来ても編集画面が描画できる必要があるため)
func (f FeatureFlag) VariationOptions() []string {
	m := f.parseVariations()
	if len(m) == 0 {
		return []string{"off", "on"}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// IsBooleanToggle は、Variationsが「キーが{on,off}ちょうど2つ、値がbooleanのtrue/false」
// という既存3フラグの形をしているかを判定する
// CONTRACT.mdセクション19.6: この形の場合だけ既存のON/OFFトグルUIを維持し、
// それ以外(3値フラグ等)は汎用のselect UIで描画する
func (f FeatureFlag) IsBooleanToggle() bool {
	m := f.parseVariations()
	if len(m) == 0 {
		// Variations未設定の古い行は、従来通りON/OFFトグル扱いにする(後方互換)
		return true
	}
	if len(m) != 2 {
		return false
	}
	onRaw, hasOn := m["on"]
	offRaw, hasOff := m["off"]
	if !hasOn || !hasOff {
		return false
	}
	var onVal, offVal bool
	if err := json.Unmarshal(onRaw, &onVal); err != nil || !onVal {
		return false
	}
	if err := json.Unmarshal(offRaw, &offVal); err != nil || offVal {
		return false
	}
	return true
}

func (f FeatureFlag) parseVariations() map[string]json.RawMessage {
	if f.Variations == nil || *f.Variations == "" {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*f.Variations), &m); err != nil {
		return nil
	}
	return m
}

// FeatureFlagAuditLog は1回の変更操作を表す監査ログ1行
// before_*/after_*両方を残すことで、画面を作らずともSQLだけで差分の履歴を追える
type FeatureFlagAuditLog struct {
	ID            uint64 `gorm:"column:id;primaryKey"`
	FeatureFlagID uint64 `gorm:"column:feature_flag_id"`
	// FlagKey は非正規化
	// 将来flag自体が削除されても履歴として読めるようにするため
	FlagKey                string  `gorm:"column:flag_key"`
	BeforeDefaultVariation *string `gorm:"column:before_default_variation"`
	AfterDefaultVariation  string  `gorm:"column:after_default_variation"`
	BeforeEnabled          *bool   `gorm:"column:before_enabled"`
	AfterEnabled           bool    `gorm:"column:after_enabled"`
	// ChangedBy はBasic Authで認証したユーザー名(本番のOIDCユーザーではない
	// この管理アプリはCONTRACT.mdセクション13の判断によりOIDCを導入していないため)
	ChangedBy string    `gorm:"column:changed_by"`
	ChangedAt time.Time `gorm:"column:changed_at"`
}

func (FeatureFlagAuditLog) TableName() string {
	return "feature_flag_audit_logs"
}
