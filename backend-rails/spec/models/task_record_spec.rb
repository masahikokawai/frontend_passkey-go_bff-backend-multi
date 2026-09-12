require "rails_helper"

RSpec.describe TaskRecord, type: :model do
  let(:user) { User.create!(keycloak_sub: "sub-1", email: "u1@example.com", name: "U1", role: 1) }

  it "nameが無いと無効(backend/internal/model Taskのバリデーションと同じ)" do
    task = user.tasks.new(status: "waiting", finished_on: Date.today)
    expect(task).not_to be_valid
    expect(task.errors[:name]).to be_present
  end

  it "nameが20文字を超えると無効(CONTRACT.mdセクション5.1・tasks.name VARCHAR(20)と同じ)" do
    task = user.tasks.new(name: "a" * 21, status: "waiting", finished_on: Date.today)
    expect(task).not_to be_valid
    expect(task.errors[:name]).to include("is too long (maximum is 20 characters)")
  end

  it "nameが20文字ちょうどなら有効" do
    task = user.tasks.new(name: "a" * 20, status: "waiting", finished_on: Date.today)
    expect(task).to be_valid
  end

  # 【2回目のテスト監査で追加】backend(Go)は`len([]rune(name))`(コードポイント数)で20文字を
  # 判定している。RubyのString#lengthはUTF-8エンコーディング下ではコードポイント数を正しく返す
  # (JVMのString#length(UTF-16コード単位数)とは違う)ため、絵文字等の基本多言語面外の文字でも
  # 期待通りのはずだが、この境界値がテストされていなかったため追加する。
  # (実際にbackend-scala-http4sではUTF-16コード単位数で数えてしまう契約不一致が見つかった)
  it "基本多言語面外の文字(絵文字)20文字なら有効(コードポイント数で判定されている)" do
    task = user.tasks.new(name: "😀" * 20, status: "waiting", finished_on: Date.today)
    expect(task).to be_valid
  end

  it "基本多言語面外の文字(絵文字)21文字は無効" do
    task = user.tasks.new(name: "😀" * 21, status: "waiting", finished_on: Date.today)
    expect(task).not_to be_valid
    expect(task.errors[:name]).to include("is too long (maximum is 20 characters)")
  end

  # 【テストカバレッジ監査で発覚した実際のバグ】Go(service/task.go validateTaskInput)・
  # Rust(model.rs validate_task_input)・Scala両実装(TaskService.validate)は全て
  # finished_onの過去日を拒否するが、このモデルには元々このバリデーションが存在せず、
  # 過去日でもTaskが作成できてしまっていた。他実装とのワイヤー契約パリティのため追加。
  it "finished_onが過去日だと無効(他実装(Go/Rust/Scala)とのワイヤー契約パリティ)" do
    task = user.tasks.new(name: "t1", status: "waiting", finished_on: Date.yesterday)
    expect(task).not_to be_valid
    expect(task.errors[:finished_on].join).to include("過去日")
  end

  it "finished_onが今日ちょうどなら有効(過去日チェックの境界値)" do
    task = user.tasks.new(name: "t1", status: "waiting", finished_on: Date.current)
    expect(task).to be_valid
  end

  it "statusはwaiting/work_in_progress/completedの3値(enum.goのTaskStatusと同じ数値対応)" do
    expect(TaskRecord.statuses).to eq("waiting" => 1, "work_in_progress" => 2, "completed" => 3)
  end

  it "as_contract_jsonがCONTRACT.mdセクション5.1のJSON形状を返す" do
    label = Label.create!(name: "urgent")
    # has_many :through(labels)は未保存レコードにlabel_ids=すると失敗するため
    # (tasks_controller.rbのcreateコメント参照)、保存後に紐付ける
    task = user.tasks.create!(name: "t1", status: "waiting", finished_on: Date.new(2030, 1, 1))
    task.label_ids = [label.id]

    json = task.as_contract_json
    expect(json).to include(
      id: task.id, name: "t1", description: nil, status: "waiting",
      finished_on: "2030-01-01", user_id: user.id, labels: [{ id: label.id, name: "urgent" }]
    )
    expect(json[:created_at]).to match(/\A\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\z/) # RFC3339
  end
end
