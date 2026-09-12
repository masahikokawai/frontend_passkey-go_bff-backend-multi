-- 実機デバッグで発覚した重大な不具合の修正: go-webauthnは登録時に記録された
-- Backup Eligible(BE)フラグと、ログイン時に認証器が実際に申告するBEフラグの
-- 一貫性を検証する("Backup Eligible flag inconsistency detected"で拒否される)。
-- webauthn_credentialsテーブルにこのフラグを保存するカラムが無く、常にfalse(ゼロ値)
-- として扱われていたため、iCloudキーチェーン・Googleパスワードマネージャー等
-- クラウド同期されるパスキー(BE=true)では、登録直後のログインが必ず失敗していた
-- (CDPの仮想認証器はBE=falseを返すため、e2eテストでは再現しなかった)。
ALTER TABLE webauthn_credentials
  ADD COLUMN backup_eligible TINYINT(1) NOT NULL DEFAULT 0 AFTER sign_count,
  ADD COLUMN backup_state    TINYINT(1) NOT NULL DEFAULT 0 AFTER backup_eligible;
