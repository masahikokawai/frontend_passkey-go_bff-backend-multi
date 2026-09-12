package model

import "time"

// Task は tasks テーブルの写し
// Rails: app/models/task.rb (training-go/gin/internal/model/task.goと同一仕様)
type Task struct {
	ID uint64 `gorm:"column:id;primaryKey"`
	// Rails: validates :name, presence: true, length: { maximum: 20 }
	Name string `gorm:"column:name"`
	// Rails: text 'description'(NULL許容)
	// 「未入力」と「空文字」を区別するため *string
	Description *string    `gorm:"column:description"`
	Status      TaskStatus `gorm:"column:status"`
	// Rails: t.date 'finished_on'
	// Goには「時刻を持たない日付型」が無いため
	// time.Timeをそのまま使い、時刻部分は0時0分0秒とする規約
	FinishedOn time.Time `gorm:"column:finished_on"`
	UserID     uint64    `gorm:"column:user_id"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`

	// belongs_to :user
	User *User `gorm:"foreignKey:UserID"`
	// has_many :labels, through: :task_labels
	Labels []Label `gorm:"many2many:task_labels;joinForeignKey:TaskID;joinReferences:LabelID"`
}

func (Task) TableName() string {
	return "tasks"
}
