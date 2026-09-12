-- CONTRACT.mdセクション13: Feature FlagのDB化。
-- 従来はflags.yaml(ファイル)で管理していたが、admin画面(admin/go, admin/rails)から
-- 再デプロイ無しでクイックに切り替えられるよう、MySQLへ正本データを移す。
CREATE TABLE feature_flags (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  flag_key VARCHAR(255) NOT NULL,
  description TEXT,
  default_variation VARCHAR(50) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY index_feature_flags_on_flag_key (flag_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- 誰が・いつ・どう変更したかの監査ログ。admin/go・admin/railsが変更のたびに1行追記する。
-- flag_keyを非正規化して持つのは、flag自体が削除された後も履歴だけは読めるようにするため。
CREATE TABLE feature_flag_audit_logs (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
