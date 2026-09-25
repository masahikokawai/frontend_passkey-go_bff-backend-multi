UPDATE feature_flags
SET variations = JSON_REMOVE(
    variations,
    '$.java', '$.kotlin', '$.python', '$.elixir', '$.haskell'
)
WHERE flag_key = 'backend.task-language';
