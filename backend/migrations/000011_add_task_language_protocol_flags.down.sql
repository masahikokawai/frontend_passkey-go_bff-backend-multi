UPDATE feature_flags
SET description = 'BFFがTask系リクエストをbackend v1(REST, N+1あり)/v2(gRPC, Preload最適化)のどちらへルーティングするかを決めるRelease Toggle。BFF内部限定、Reactには非公開。'
WHERE flag_key = 'bff.tasks-backend-v2';

DELETE FROM feature_flags WHERE flag_key = 'backend.task-protocol';
DELETE FROM feature_flags WHERE flag_key = 'backend.task-language';
