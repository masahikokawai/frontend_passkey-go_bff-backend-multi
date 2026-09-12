-- CONTRACT.mdセクション24: 外部公開API(/external/v1/tasks)のTask一覧取得ロジックを
-- GORM実装/bob実装のどちらで動かすか切り替えるRelease Toggle。
-- 既存の backend.external-tasks-pagination-v2(offset/cursor)とは独立した軸のため、
-- セクション19.1・20.2の原則通り別フラグにする(掛け合わせで4パターン全てに到達可能)。
INSERT INTO feature_flags (flag_key, description, default_variation, enabled, variations, created_at, updated_at)
VALUES (
  'backend.external-tasks-orm',
  '外部公開API(/external/v1/tasks)のTask一覧取得を、GORM実装/bob(https://github.com/stephenafamo/bob)実装のどちらで動かすかを切り替えるRelease Toggle。backend.external-tasks-pagination-v2(offset/cursor)とは独立した軸で、backend自身が直接MySQLを評価する。レスポンスのJSON形状はどちらの実装でも完全に同一。',
  'gorm',
  1,
  JSON_OBJECT('gorm', 'gorm', 'bob', 'bob'),
  NOW(),
  NOW()
);
