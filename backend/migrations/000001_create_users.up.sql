-- Rails db/schema.rb の users テーブルが元。
-- Rails版と異なり、認証はKeycloak(OIDC)に委譲したため password関連の列は持たない
-- (CONTRACT.md セクション10参照)。代わりにKeycloakの Subject(sub) クレームで
-- 1ユーザーを一意に特定する keycloak_sub 列を追加した。
-- role は Rails の `enum role: { general: 1, management: 2 }` を踏襲する。
CREATE TABLE users (
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
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;
