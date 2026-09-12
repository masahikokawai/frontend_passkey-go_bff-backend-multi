# task_labels 中間テーブルの写し(backend/internal/model/task_label.go)
class TaskLabel < ActiveRecord::Base
  belongs_to :task, class_name: "TaskRecord", foreign_key: :task_id
  belongs_to :label
end
