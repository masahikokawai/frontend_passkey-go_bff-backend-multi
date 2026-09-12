DELETE FROM feature_flags WHERE flag_key IN (
  'frontend.tasks-ts-rewrite',
  'bff.tasks-backend-v2',
  'backend.external-tasks-pagination-v2'
);
