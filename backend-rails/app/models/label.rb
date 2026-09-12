# labels テーブルの写し(backend/internal/model/label.go)
class Label < ActiveRecord::Base
  has_many :task_labels, dependent: :destroy
  has_many :tasks, through: :task_labels, class_name: "TaskRecord", source: :task

  validates :name, presence: true
end
