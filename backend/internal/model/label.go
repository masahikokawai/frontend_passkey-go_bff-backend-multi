package model

import "time"

// Label は labels テーブルの写し
// Rails: app/models/label.rb
type Label struct {
	ID        uint64    `gorm:"column:id;primaryKey"`
	Name      string    `gorm:"column:name"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`

	// has_many :tasks, through: :task_labels
	Tasks []Task `gorm:"many2many:task_labels;joinForeignKey:LabelID;joinReferences:TaskID"`
}

func (Label) TableName() string {
	return "labels"
}
