# tasks テーブルの写し(backend/internal/model/task.go、CONTRACT.mdセクション5.1のJSON契約)
# Rails: validates :name, presence: true, length: { maximum: 20 } は
# 既存 training-go/gin の Task モデルおよび backend(Go)側の同等バリデーションと同一仕様。
#
# 【実装中に発覚した実際の衝突】protoc-gen-ruby(grpc_tools_ruby_protoc)は
# proto の package `task.v1` から Ruby の名前空間を `Task::V1::...` として生成する
# (google-protobufのRuby実装の固定の命名規則で、.protoへ変更を加えても回避策が無い
# ruby_package相当のファイルオプションはRubyには存在しない)。
# ActiveRecordモデルを素直に `class Task` としてしまうと、生成コード側が
# 最上位定数 `Task` をモジュールとして定義しようとして衝突し
# (`TypeError: Task is not a module` 等)、どちらを先にrequireしても起動時エラーになる。
# そのためこのモデルはあえて `TaskRecord` と命名し、テーブル名だけ `tasks` を指定する
# (Go/Rust/Scala実装には無い、Rails+protoc-gen-ruby特有の制約)。
class TaskRecord < ActiveRecord::Base
  self.table_name = "tasks"

  # DB上はTINYINT UNSIGNED(1=waiting/2=work_in_progress/3=completed)。
  # backend/internal/model/enum.go の TaskStatus と同じ数値対応。
  enum :status, { waiting: 1, work_in_progress: 2, completed: 3 }, validate: true

  belongs_to :user
  has_many :task_labels, foreign_key: :task_id, dependent: :destroy
  has_many :labels, through: :task_labels

  validates :name, presence: true, length: { maximum: 20 }
  validates :finished_on, presence: true
  # 【テストカバレッジ監査で発覚した実際のバグ・追記】Go(backend/internal/service/task.go
  # validateTaskInput)・Rust(backend-rust/src/model.rs validate_task_input)・
  # Scala両実装(TaskService.validate)は全てfinished_onの過去日を422 validation_errorで
  # 拒否しているが、Rails版だけこのチェックが無く、過去日をそのまま作成できてしまっていた
  # (ワイヤー契約パリティ違反)。他実装と同じ「今日はOK、今日より前はNG」の境界で揃える。
  validate :finished_on_not_in_past

  # CONTRACT.mdセクション5.1のJSON形状に合わせる(スネークケース、statusは文字列、
  # finished_onはYYYY-MM-DD、created_at/updated_atはRFC3339)。
  # bffのtaskV1DTOはこのケース・形式でアンマーシャルするため、ここを崩すと
  # (Go実装で統合時に発覚したのと同種の)不整合が起きる。
  def as_contract_json
    {
      id: id,
      name: name,
      description: description,
      status: status,
      finished_on: finished_on.strftime("%Y-%m-%d"),
      user_id: user_id,
      labels: labels.map { |l| { id: l.id, name: l.name } },
      created_at: created_at.utc.iso8601,
      updated_at: updated_at.utc.iso8601
    }
  end

  # 外部公開API(CONTRACT.mdセクション11)用のJSON形状。内部CRUDと違い`user_id`を含めない
  # (backend/internal/handler/external/task.goのtaskDTOToJSONと同一、Go実装の実際の挙動に合わせた)。
  def as_external_contract_json
    as_contract_json.except(:user_id)
  end

  private

  # 【テストカバレッジ監査で発覚した実際のバグ】Go(backend/internal/service/task.go
  # validateTaskInput)・Rust(backend-rust/src/model.rs validate_task_input)・
  # Scala両実装(TaskService.validate)は全てfinished_onの過去日を422 validation_errorで
  # 拒否しているが、Rails版だけこのチェックが無く、過去日をそのまま作成できてしまっていた
  # (ワイヤー契約パリティ違反)。他実装と同じ「今日はOK、今日より前はNG」の境界で揃える。
  def finished_on_not_in_past
    return if finished_on.blank? # presence: trueが既に別エラーを出すのでここでは何もしない

    errors.add(:finished_on, "に過去日は指定できません") if finished_on < Date.current
  end
end
