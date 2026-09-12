# FeatureFlag は backend(training-go/bff-gin/backend)のgolang-migrateが正本として
# 管理する feature_flags テーブルへ接続するだけのモデル
# このRailsアプリ自身はこのテーブルを作成するマイグレーションを持たない(CONTRACT.mdセクション13参照)
#
# Go側(admin/go)との比較学習の題材: 同じテーブルに対してGo(GORM)とRails(ActiveRecord)
# それぞれからCRUDする実装を用意している
class FeatureFlag < ApplicationRecord
  self.table_name = "feature_flags"

  has_many :feature_flag_audit_logs, dependent: nil

  validates :flag_key, presence: true, uniqueness: true
  # CONTRACT.mdセクション19で変更: 以前は "on"/"off" の2値決め打ちだったが、
  # 多値(multivariate)フラグ対応のため、そのフラグ自身のvariationsに定義された
  # キーのいずれかであることを検証する(admin/goのVariationOptionsと同じ考え方)
  validates :default_variation, presence: true, inclusion: { in: ->(record) { record.variation_options } }

  # variation_options はvariationsカラム(JSON)をパースし、キー一覧をソート済みで返す
  # 未設定(NULL)・パース不能の場合は既存フラグ相当の["off","on"]にフォールバックする
  # (variationsカラム追加前の古い行、または移行漏れへの後方互換)
  def variation_options
    v = variations
    return %w[off on] if v.blank?

    v.keys.sort
  end

  # boolean_toggle? は、variationsが「キーが{on,off}ちょうど2つ、値がtrue/false」という
  # 既存フラグの形をしているかを判定する(admin/goのIsBooleanToggleと同じ考え方)
  # この場合だけ既存のON/OFFトグルUIを維持し、それ以外(3値フラグ等)は汎用select UIで描画する
  def boolean_toggle?
    v = variations
    return true if v.blank? # 未設定の古い行は従来通りON/OFFトグル扱い(後方互換)
    return false if v.size != 2

    v["on"] == true && v["off"] == false
  end
end
