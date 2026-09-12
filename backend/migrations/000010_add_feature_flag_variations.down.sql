DELETE FROM feature_flags WHERE flag_key = 'frontend.task-create-ux';

ALTER TABLE feature_flags DROP COLUMN variations;
