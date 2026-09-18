UPDATE feature_flags
SET variations = JSON_REMOVE(variations, '$.javascript', '$.typescript')
WHERE flag_key = 'backend.task-language';
