-- CONTRACT.mdセクション20: backend.task-languageにJava/Kotlin/Python/Elixir/Haskell
-- (backend-java/backend-kotlin/backend-python/backend-elixir/backend-haskell、
-- Task CRUDの内部REST v1+内部gRPC v2+JWT/JWKS認証を実装済み)の5言語を追加する
-- 既存の選択肢(go/rust/scala-http4s/scala-pekko/rails/javascript/typescript/cpp/c)はそのまま残し、
-- 5つ追加するだけの変更
UPDATE feature_flags
SET variations = JSON_SET(
    variations,
    '$.java', 'java',
    '$.kotlin', 'kotlin',
    '$.python', 'python',
    '$.elixir', 'elixir',
    '$.haskell', 'haskell'
)
WHERE flag_key = 'backend.task-language';
