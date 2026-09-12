-- CONTRACT.md セクション16.2: 認証情報(Keycloak/ローカルパスワード)をusersから分離する。
-- Railsの元設計(User + UserCredential)に倣い、認証方式ごとに別テーブルへ切り出す。
CREATE TABLE user_passwords (
  id                  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id             BIGINT UNSIGNED NOT NULL,
  password_digest     VARCHAR(255) NOT NULL,
  password_expires_at DATETIME NOT NULL,
  created_at          DATETIME NOT NULL,
  updated_at          DATETIME NOT NULL,
  UNIQUE KEY idx_user_passwords_user_id (user_id),
  CONSTRAINT fk_user_passwords_user_id FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE user_keycloaks (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id      BIGINT UNSIGNED NOT NULL,
  keycloak_sub VARCHAR(255) NOT NULL,
  created_at   DATETIME NOT NULL,
  updated_at   DATETIME NOT NULL,
  UNIQUE KEY idx_user_keycloaks_user_id (user_id),
  UNIQUE KEY idx_user_keycloaks_keycloak_sub (keycloak_sub),
  CONSTRAINT fk_user_keycloaks_user_id FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 既存データ移行: users.keycloak_subが入っている行をuser_keycloaksへコピーしてから、
-- usersからkeycloak_subカラム自体を落とす(CONTRACT.md セクション16.2)。
INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at)
SELECT id, keycloak_sub, NOW(), NOW() FROM users WHERE keycloak_sub IS NOT NULL AND keycloak_sub <> '';

ALTER TABLE users DROP COLUMN keycloak_sub;
