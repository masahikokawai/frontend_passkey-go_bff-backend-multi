package model

import "time"

// TaskLabel は task_labels テーブル(中間テーブル)の写し
// GORMのmany2many機能がINSERT/DELETEを自動で行うため、通常のクエリではこの構造体を直接使わない
// テーブル定義をコードとして残すためのドキュメント的な存在(training-go/gin/internal/model/task_label.goと同一方針)
type TaskLabel struct {
	ID        uint64    `gorm:"column:id;primaryKey"`
	TaskID    uint64    `gorm:"column:task_id"`
	LabelID   uint64    `gorm:"column:label_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (TaskLabel) TableName() string {
	return "task_labels"
}
