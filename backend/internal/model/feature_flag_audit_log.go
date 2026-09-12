package model

import "time"

// FeatureFlagAuditLog は feature_flag_audit_logs テーブルの写し
// admin/go・admin/railsがフラグを変更するたびに1行追記する「誰が・いつ・何を変更したか」の履歴
// FlagKeyを非正規化して持つのは、対象のFeatureFlag行が削除された後も履歴だけは読めるようにするため
type FeatureFlagAuditLog struct {
	ID                     uint64    `gorm:"column:id;primaryKey"`
	FeatureFlagID          uint64    `gorm:"column:feature_flag_id"`
	FlagKey                string    `gorm:"column:flag_key"`
	BeforeDefaultVariation *string   `gorm:"column:before_default_variation"`
	AfterDefaultVariation  string    `gorm:"column:after_default_variation"`
	BeforeEnabled          *bool     `gorm:"column:before_enabled"`
	AfterEnabled           bool      `gorm:"column:after_enabled"`
	ChangedBy              string    `gorm:"column:changed_by"`
	ChangedAt              time.Time `gorm:"column:changed_at"`
}

func (FeatureFlagAuditLog) TableName() string {
	return "feature_flag_audit_logs"
}
