package model

import "time"

// FeatureFlag は feature_flags テーブルの写し
// CONTRACT.mdセクション13参照
// 従来はbff/backendそれぞれのflags.yaml(ファイル)で管理していたが、admin画面
// (admin/go, admin/rails)から再デプロイ無しで切り替えられるようMySQLへ移した
//
// Rails対比: このプロジェクトにRails版は無いが、admin/railsではこのテーブルを
// ActiveRecordモデルとして扱う(スキーマはbackend側のgolang-migrateが正本)
type FeatureFlag struct {
	ID               uint64 `gorm:"column:id;primaryKey"`
	FlagKey          string `gorm:"column:flag_key"`
	Description      string `gorm:"column:description"`
	DefaultVariation string `gorm:"column:default_variation"`
	// Variations はGO Feature Flagが読む「1フラグあたりのvariations定義」をJSON文字列
	// のまま保持する(例: `{"on":true,"off":false}` や `{"inline":"inline","modal":"modal","page":"page"}`)
	//
	// CONTRACT.mdセクション19: 以前はbackend/internal/featureflag/mysql_retriever.goが
	// 全フラグ一律で`{"on":true,"off":false}`と決め打ちしていたが、複数のUXパターンから
	// 1つを選ぶような多値(multivariate)フラグに対応するため、DB側に定義を持たせる形へ一般化した
	//
	// 空文字列/NULLの場合は既存の決め打ち値にフォールバックする(古い行との後方互換)
	Variations string    `gorm:"column:variations"`
	Enabled    bool      `gorm:"column:enabled"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (FeatureFlag) TableName() string {
	return "feature_flags"
}
