# feature_flags テーブルの読み取り専用の写し(backend/internal/model/feature_flag.go)
# この Railsアプリは admin/go・admin/rails のような管理UIを持たず、
# あくまで backend.external-tasks-pagination-v2 を読むためだけに使う(CONTRACT.mdセクション20.9)
# スキーマは backend の golang-migrate(migrations/000006_create_feature_flags)が正本
class FeatureFlag < ActiveRecord::Base
  # Go実装(backend/internal/featureflag/mysql_retriever.go)の評価ロジックのうち、
  # 「boolean(on/off)フラグ1つの現在値」だけを再現する
  # variations列の中身(JSON)まで一般的に解釈する必要はなく(このアプリが読むのはbackend.external-tasks-pagination-v2の1つだけ)、
  # enabled/default_variationの2列だけで十分
  #
  # enabled=false の場合、Go実装は呼び出し側が渡すdefaultValue(常にfalse)を返す設計
  # (このプロジェクトの各フラグは defaultRule のみでtargetingルールを持たないため)
  def self.boolean_enabled?(flag_key)
    flag = find_by(flag_key: flag_key)
    return false if flag.nil? || !flag.enabled?

    flag.default_variation == "on"
  end
end
