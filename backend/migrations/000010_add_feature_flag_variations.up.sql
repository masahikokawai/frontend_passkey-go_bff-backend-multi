-- CONTRACT.mdセクション19: Task登録UXの3パターン比較のため、多値(multivariate)の
-- Feature Flagを追加する。既存3フラグはboolean(on/off)の2択だったが、今回は
-- 「inline/modal/page」という3つの排他的なUXパターンから1つを選ぶため、
-- 独立したbooleanフラグを3つ用意するアンチパターンを避け、1つのフラグに
-- 複数のvariationsを持たせる設計にする。
--
-- variationsカラムはGO Feature Flagが読む「1フラグあたりのvariations」定義
-- ({"on": true, "off": false} のような形)をJSONでそのまま保持する。
-- 従来はbackend/internal/featureflag/mysql_retriever.goのBuildFlagConfigJSONが
-- この値を `map[string]bool{"on": true, "off": false}` と決め打ちしていたが、
-- 今回この決め打ちをやめ、DBの値をそのまま使うよう一般化する。
ALTER TABLE feature_flags ADD COLUMN variations JSON NULL AFTER default_variation;

-- 既存3フラグは、決め打ちされていた値と全く同じ内容をバックフィルする
-- (BuildFlagConfigJSONの出力結果が変更前後で変わらないようにするため、後方互換の要)。
UPDATE feature_flags SET variations = JSON_OBJECT('on', true, 'off', false)
WHERE flag_key IN ('frontend.tasks-ts-rewrite', 'bff.tasks-backend-v2', 'backend.external-tasks-pagination-v2');

-- 新規: Task登録UXの3パターン切り替えフラグ。既定は現行の"inline"(一覧上部の常設フォーム)。
INSERT INTO feature_flags (flag_key, description, default_variation, enabled, variations, created_at, updated_at)
VALUES (
  'frontend.task-create-ux',
  'Task登録UIの3パターン(inline: 一覧上部の常設フォーム / modal: モーダル / page: 別ページ遷移)を切り替えるRelease Toggle。frontendが評価し/api/me経由でReactへ渡す。',
  'inline',
  1,
  JSON_OBJECT('inline', 'inline', 'modal', 'modal', 'page', 'page'),
  NOW(),
  NOW()
);
