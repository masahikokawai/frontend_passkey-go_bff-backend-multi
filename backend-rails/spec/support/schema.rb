# このRailsアプリはtasks/labels/task_labels/usersテーブルの作成マイグレーションを
# 持たない(schema owner はbackendのgolang-migrate。CONTRACT.mdセクション20.3参照)
# そのためtest環境(backend_rails_test、config/database.yml参照。共有DBとは別)でだけ、
# specスイート開始時に生SQLで同じスキーマを再現する(admin/rails/spec/support/schema.rbと同じ方針)
RSpec.configure do |config|
  config.before(:suite) do
    connection = ActiveRecord::Base.connection

    connection.execute(<<~SQL)
      CREATE TABLE IF NOT EXISTS users (
        id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        keycloak_sub VARCHAR(255)    NOT NULL,
        email        VARCHAR(255)    NOT NULL,
        name         VARCHAR(255)    NOT NULL,
        role         TINYINT UNSIGNED NOT NULL DEFAULT 1,
        created_at   DATETIME        NOT NULL,
        updated_at   DATETIME        NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY index_users_on_keycloak_sub (keycloak_sub),
        UNIQUE KEY index_users_on_email (email)
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
    SQL

    connection.execute(<<~SQL)
      CREATE TABLE IF NOT EXISTS labels (
        id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        name       VARCHAR(255)    NOT NULL,
        created_at DATETIME        NOT NULL,
        updated_at DATETIME        NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY index_labels_on_name (name)
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
    SQL

    connection.execute(<<~SQL)
      CREATE TABLE IF NOT EXISTS tasks (
        id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        name        VARCHAR(20)     NOT NULL,
        description TEXT            NULL,
        status      TINYINT UNSIGNED NOT NULL DEFAULT 1,
        finished_on DATE            NOT NULL,
        user_id     BIGINT UNSIGNED NOT NULL,
        created_at  DATETIME        NOT NULL,
        updated_at  DATETIME        NOT NULL,
        PRIMARY KEY (id),
        KEY index_tasks_on_finished_on (finished_on),
        KEY index_tasks_on_status (status),
        KEY index_tasks_on_user_id (user_id)
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
    SQL

    connection.execute(<<~SQL)
      CREATE TABLE IF NOT EXISTS task_labels (
        id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        task_id    BIGINT UNSIGNED NOT NULL,
        label_id   BIGINT UNSIGNED NOT NULL,
        created_at DATETIME        NOT NULL,
        updated_at DATETIME        NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY index_task_label_on_uniq_key (task_id, label_id),
        KEY index_task_labels_on_label_id (label_id)
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
    SQL

    # CONTRACT.mdセクション11・20.9: 外部公開APIのbackend.external-tasks-pagination-v2を
    # 読むためだけに必要(backend/migrations/000006_create_feature_flagsと同一スキーマ、
    # このアプリはvariations/feature_flag_audit_logsまでは使わないため作らない)。
    connection.execute(<<~SQL)
      CREATE TABLE IF NOT EXISTS feature_flags (
        id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        flag_key VARCHAR(255) NOT NULL,
        description TEXT,
        default_variation VARCHAR(50) NOT NULL,
        enabled TINYINT(1) NOT NULL DEFAULT 1,
        created_at DATETIME NOT NULL,
        updated_at DATETIME NOT NULL,
        PRIMARY KEY (id),
        UNIQUE KEY index_feature_flags_on_flag_key (flag_key)
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
    SQL

    # 前回実行の残骸を掃除(AUTO_INCREMENTの巻き戻しも兼ねる)
    connection.execute("SET FOREIGN_KEY_CHECKS = 0")
    %w[task_labels tasks labels users feature_flags].each { |t| connection.execute("TRUNCATE TABLE #{t}") }
    connection.execute("SET FOREIGN_KEY_CHECKS = 1")
  end
end
