UPDATE feature_flags
SET variations = JSON_REMOVE(variations, '$.cpp')
WHERE flag_key = 'backend.task-language';
