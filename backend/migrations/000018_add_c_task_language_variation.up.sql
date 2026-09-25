-- CONTRACT.mdセクション20: backend.task-languageにC実装
-- (backend-c、Task CRUDの内部REST v1+内部gRPC v2+JWT/JWKS認証を実装済み。
-- 外部公開APIは未実装のため、gateway側の接続先は用意するが実際には機能しない)を追加する
-- 既存の選択肢(go/rust/scala-http4s/scala-pekko/rails/javascript/typescript/cpp)はそのまま残し、1つ追加するだけの変更
UPDATE feature_flags
SET variations = JSON_SET(variations, '$.c', 'c')
WHERE flag_key = 'backend.task-language';
