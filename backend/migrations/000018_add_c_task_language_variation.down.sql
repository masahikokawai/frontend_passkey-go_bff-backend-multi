UPDATE feature_flags
SET variations = JSON_REMOVE(variations, '$.c')
WHERE flag_key = 'backend.task-language';
