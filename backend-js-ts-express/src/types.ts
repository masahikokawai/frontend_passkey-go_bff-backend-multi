// backend-jsには無い、TypeScript版だけの追加ファイル。
// backend-js全体で使う共有の型定義をここに集約する(JS版には型注釈が無いため対応物が無い)。

export interface Label {
  id: number;
  name: string;
}

// MySQLの行(dateStrings:true)から素直に組み立てたTaskの内部表現。
// finishedOn/createdAt/updatedAtは常にUTC基準の文字列のまま扱う(src/time.ts参照)。
export interface Task {
  id: number;
  name: string;
  description: string | null;
  status: number;
  finishedOn: string;
  userId: number;
  createdAt: string;
  updatedAt: string;
  labels: Label[];
}

export interface TaskInput {
  name: string;
  description: string | null;
  status: string;
  finishedOn: string;
  labelIds: number[];
}

export interface JwtClaims {
  iss: string;
  sub: string;
  aud?: string | string[];
  azp?: string;
  [key: string]: unknown;
}

export interface AppConfig {
  httpAddr: string;
  grpcAddr: string;
  externalHttpAddr: string;
  externalApiClientId: string;
  featureFlagPollIntervalSecs: number;
  dbDsn: string;
  keycloakIssuer: string;
  keycloakJwksUrl: string;
  expectedAudience: string;
  localHmacSecret: string;
  localRsaJwksUrl: string;
  logLevel: string;
}

// 内部REST v1・gRPC v2で共有するリクエストスコープの状態
// (mysql2のPool型・Dispatcher型は循環import回避のためunknownで受け、使用箇所でimportし直す)
export interface AppState {
  pool: import('mysql2/promise').Pool;
  dispatcher: import('./auth/jwt').Dispatcher;
}

// 外部公開API専用の状態
export interface ExternalState {
  pool: import('mysql2/promise').Pool;
  keycloakVerifier: import('./auth/jwt').JwksVerifier;
  expectedClientId: string;
  paginationV2Flag: import('./flags').FlagCache;
}
