# users テーブルの写し(backend/internal/model/user.go、CONTRACT.mdセクション10)
# 認証はKeycloak(OIDC)に委譲しているためパスワード関連の列は持たない
class User < ActiveRecord::Base
  ROLES = { general: 1, management: 2 }.freeze

  # class_name指定必須(app/models/task_record.rbのコメント参照: 素の`Task`定数は
  # protoc生成コードの`Task::V1::...`名前空間と衝突するため使えない)
  has_many :tasks, class_name: "TaskRecord", foreign_key: :user_id, dependent: :destroy

  def role_name
    ROLES.key(role)
  end
end
