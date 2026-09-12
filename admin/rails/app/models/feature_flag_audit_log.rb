# feature_flag_audit_logs も backend が正本として管理するテーブル(FeatureFlag参照)
# flag_keyを非正規化して持っているので、対象のFeatureFlagが後から削除されても
# 履歴だけは読める(このRailsアプリではFeatureFlagの削除機能自体は提供しない想定だが、
# 念のための設計)
class FeatureFlagAuditLog < ApplicationRecord
  self.table_name = "feature_flag_audit_logs"

  belongs_to :feature_flag

  validates :flag_key, presence: true
  validates :after_default_variation, presence: true
  validates :changed_by, presence: true
end
