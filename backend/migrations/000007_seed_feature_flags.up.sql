-- 既存のflags.yaml(bff/backend双方)と同じ内容・同じ既定値(default_variation="off", enabled=1)で
-- 初期データを投入する。
INSERT INTO feature_flags (flag_key, description, default_variation, enabled, created_at, updated_at) VALUES
  ('frontend.tasks-ts-rewrite', 'React側でTask一覧の新実装(TypeScript)/旧実装(JS, legacy/)を切り替えるRelease Toggle。bffが評価し/api/me経由でReactへ渡す。', 'off', 1, NOW(), NOW()),
  ('bff.tasks-backend-v2', 'BFFがTask系リクエストをbackend v1(REST, N+1あり)/v2(gRPC, Preload最適化)のどちらへルーティングするかを決めるRelease Toggle。BFF内部限定、Reactには非公開。', 'off', 1, NOW(), NOW()),
  ('backend.external-tasks-pagination-v2', 'BFF非経由の外部公開API(/external/v1/tasks)のページング方式(offset/cursor)を切り替える。backend自身が評価する。', 'off', 1, NOW(), NOW());
