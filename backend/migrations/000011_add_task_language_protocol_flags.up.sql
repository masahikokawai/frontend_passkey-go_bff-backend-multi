-- CONTRACT.mdセクション20: backendの実処理をGo以外の言語(Rust/Scala(http4s)/
-- Scala(Pekko HTTP)/Rails)でも比較実装する。言語の選択とプロトコル(REST/gRPC)の
-- 選択は互いに独立した軸(どの言語でもREST/gRPCどちらも成立する)のため、
-- セクション19.1の原則(独立した軸は別フラグ)に従い2つの多値フラグに分ける。

-- 新規: backendの実処理を担う言語の選択(内部CRUD・外部公開APIの両方で共有する)
-- 現時点ではGo以外の実装は未着手のため、default_variationは既存動作を維持する"go"のまま
INSERT INTO feature_flags (flag_key, description, default_variation, enabled, variations, created_at, updated_at)
VALUES (
  'backend.task-language',
  'backendのTask CRUD実処理を担う言語(go/rust/scala-http4s/scala-pekko/rails)を切り替えるRelease Toggle。bffの内部CRUDルーティングと外部公開APIゲートウェイの両方で同じ値を共有する(内部/外部で言語がズレないため)。go以外は段階的に実装予定(CONTRACT.mdセクション20.9)。未実装の言語が選ばれている間、bff/ゲートウェイはgoへフォールバックする。',
  'go',
  1,
  JSON_OBJECT('go', 'go', 'rust', 'rust', 'scala-http4s', 'scala-http4s', 'scala-pekko', 'scala-pekko', 'rails', 'rails'),
  NOW(),
  NOW()
);

-- 新規: bffが内部CRUDでbackendと話すプロトコルの選択(REST/gRPC)
-- 既存の`bff.tasks-backend-v2`(boolean)が担っていた役割を汎化して引き継ぐ
INSERT INTO feature_flags (flag_key, description, default_variation, enabled, variations, created_at, updated_at)
VALUES (
  'backend.task-protocol',
  'bffが内部Task CRUDでbackendと通信するプロトコル(rest/grpc)を切り替えるRelease Toggle。既存の`bff.tasks-backend-v2`(boolean)を汎化して引き継ぐもので、bffはこちらを評価する。外部公開APIは常にRESTのみのため対象外。',
  'rest',
  1,
  JSON_OBJECT('rest', 'rest', 'grpc', 'grpc'),
  NOW(),
  NOW()
);

-- 既存の`bff.tasks-backend-v2`は非推奨として残置する(admin画面の変更履歴・過去の切り替え記録を
-- 保持するため削除しない)。bffはこのフラグをもう評価しないことをdescriptionに明記する。
UPDATE feature_flags
SET description = '【非推奨・bffはもう評価しません】backend.task-protocolに統合されました。過去の変更履歴保持のため残置。'
WHERE flag_key = 'bff.tasks-backend-v2';
