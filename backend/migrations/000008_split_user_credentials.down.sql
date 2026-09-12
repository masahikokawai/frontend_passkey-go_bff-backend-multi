-- 000001の元スキーマは `keycloak_sub VARCHAR(255) NOT NULL` + UNIQUE KEYだったが、
-- ローカル認証ユーザー(user_keycloaksを持たない)が存在しうる現在の状態では
-- NOT NULLに戻すと復元不可能になるため、NULL許容のままにする(MySQLのUNIQUE KEYは
-- NULL同士を重複とみなさないため、UNIQUE制約自体は元通り復元できる)。
ALTER TABLE users ADD COLUMN keycloak_sub VARCHAR(255) NULL AFTER id;

UPDATE users
JOIN user_keycloaks ON user_keycloaks.user_id = users.id
SET users.keycloak_sub = user_keycloaks.keycloak_sub;

-- 【DB往復検証で発覚した不具合】以前はここでUNIQUE KEYを復元しておらず、
-- down後の users テーブルには元の一意制約が失われていた。
ALTER TABLE users ADD UNIQUE KEY index_users_on_keycloak_sub (keycloak_sub);

DROP TABLE user_keycloaks;
DROP TABLE user_passwords;
