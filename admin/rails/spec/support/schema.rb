# この Rails アプリは feature_flags/feature_flag_audit_logs テーブルの作成マイグレーションを持たない
# (schema owner はbackendのgolang-migrateCONTRACT.mdセクション13参照)
# そのためtest環境(admin_rails_test、config/database.yml参照。共有DBとは別)でだけ、
# specスイート開始時に生SQLで同じスキーマを再現する
RSpec.configure do |config|
  config.before(:suite) do
    connection = ActiveRecord::Base.connection

    connection.execute(<<~SQL)
      CREATE TABLE IF NOT EXISTS feature_flags (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        flag_key VARCHAR(255) NOT NULL,
        description TEXT,
        default_variation VARCHAR(50) NOT NULL,
        variations JSON NULL,
        enabled TINYINT(1) NOT NULL DEFAULT 1,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY index_feature_flags_on_flag_key (flag_key)
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
    SQL

    # CONTRACT.mdセクション19: 
    # variationsカラムを追加した
    #
    # CREATE TABLE IF NOT EXISTSは
    # 既に(古いスキーマで)テーブルが存在する場合は何もしないため、前回までのテスト実行で
    # 作られたテーブルにはこのALTERで追いカラムする
    # 【実機検証で判明】MySQLの ADD COLUMN に IF NOT EXISTS 構文は無い(構文エラーになる)ため、
    # column_exists?で事前判定してから実行する
    unless connection.column_exists?(:feature_flags, :variations)
      connection.execute("ALTER TABLE feature_flags ADD COLUMN variations JSON NULL AFTER default_variation")
    end

    connection.execute(<<~SQL)
      CREATE TABLE IF NOT EXISTS feature_flag_audit_logs (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        feature_flag_id BIGINT UNSIGNED NOT NULL,
        flag_key VARCHAR(255) NOT NULL,
        before_default_variation VARCHAR(50),
        after_default_variation VARCHAR(50) NOT NULL,
        before_enabled TINYINT(1),
        after_enabled TINYINT(1) NOT NULL,
        changed_by VARCHAR(255) NOT NULL,
        changed_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        KEY index_feature_flag_audit_logs_on_feature_flag_id (feature_flag_id)
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
    SQL

    # 各テスト後にトランザクションロールバックされるとはいえ、suite開始時点で
    # 前回実行の残骸が無いようにしておく(AUTO_INCREMENTの巻き戻しも兼ねる)
    connection.execute("SET FOREIGN_KEY_CHECKS = 0")
    connection.execute("TRUNCATE TABLE feature_flag_audit_logs")
    connection.execute("TRUNCATE TABLE feature_flags")
    connection.execute("SET FOREIGN_KEY_CHECKS = 1")
  end
end
