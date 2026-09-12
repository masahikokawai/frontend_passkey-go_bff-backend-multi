-- CONTRACT.mdセクション22: 既存ユーザー(ローカル認証)向けの追加認証手段として
-- パスキー(WebAuthn discoverable credential)を導入する。
-- credential_idをグローバルに一意なルックアップキーにする理由: discoverable
-- credentialでのログイン時、ユーザーが未特定の状態で「このcredential_idは誰のものか」を
-- 引く必要があるため(ユーザー名/メールアドレスの事前入力を要求しないため)。
CREATE TABLE webauthn_credentials (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id       BIGINT UNSIGNED NOT NULL,
    credential_id VARBINARY(1024) NOT NULL,
    public_key    BLOB            NOT NULL,
    sign_count    BIGINT UNSIGNED NOT NULL DEFAULT 0,
    transports    VARCHAR(255)    NULL,
    name          VARCHAR(255)    NULL,
    created_at    DATETIME        NOT NULL,
    updated_at    DATETIME        NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY index_webauthn_credentials_on_credential_id (credential_id),
    KEY index_webauthn_credentials_on_user_id (user_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;
