-- CONTRACT.md セクション16.2: ローカル(非Keycloak)認証の比較検証用に、
-- 登録画面を用意する代わりに1人だけ手動でseedする(実務でも最初の管理者は手動発行するのが一般的なため)
--
-- password_digestは "password" をbcrypt(cost=10)でハッシュ化した固定値
-- (マイグレーションSQL自体はbcryptを計算できないため、事前に golang.org/x/crypto/bcryptで生成した値をそのまま埋め込んでいる)
--
-- password_expires_atは2099年に設定している
-- パスワード再設定画面を今回のスコープでは用意しないため(CONTRACT.mdセクション16.7参照)、学習中に期限切れで詰まらないよう十分先の日付にする割り切り
INSERT INTO users (email, name, role, created_at, updated_at)
VALUES ('local-user@example.com', 'ローカル太郎', 1, NOW(), NOW());

INSERT INTO user_passwords (user_id, password_digest, password_expires_at, created_at, updated_at)
SELECT id, '$2a$10$FAOdHAqFL/bi9G4aPF/PDeo26Y2k8/uQFjeYgQNAf2FYr92Jo0xpG', '2099-12-31 23:59:59', NOW(), NOW()
FROM users WHERE email = 'local-user@example.com';
