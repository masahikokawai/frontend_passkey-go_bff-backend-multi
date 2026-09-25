-- CONTRACT.mdセクション20: backend.task-languageにC++実装
-- (backend-cpp、Task CRUDのREST v1+gRPC v2+外部公開APIを実装済み)を追加する
-- 既存の選択肢(go/rust/scala-http4s/scala-pekko/rails/javascript/typescript)はそのまま残し、1つ追加するだけの変更
UPDATE feature_flags
SET variations = JSON_SET(variations, '$.cpp', 'cpp')
WHERE flag_key = 'backend.task-language';
