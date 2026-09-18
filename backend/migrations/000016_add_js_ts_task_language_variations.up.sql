-- CONTRACT.mdセクション20: backend.task-languageにJavaScript/TypeScript実装
-- (backend-js-express/backend-js-ts-express、いずれもTask CRUDのREST v1+gRPC v2+外部公開APIを実装済み)を追加する
-- 既存の選択肢(go/rust/scala-http4s/scala-pekko/rails)はそのまま残し、2つ追加するだけの変更
UPDATE feature_flags
SET variations = JSON_SET(variations, '$.javascript', 'javascript', '$.typescript', 'typescript')
WHERE flag_key = 'backend.task-language';
