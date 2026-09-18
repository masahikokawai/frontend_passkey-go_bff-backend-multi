// backend-jsには無い、TypeScript版だけの追加ファイル。
// proto-loaderは実行時に動的にロードするため静的な型を生成しない(ts-protoのような別ツールは
// 導入していない。理由はREADME参照)。ここではproto/task/v1/task.protoのメッセージ形状を
// 手書きで型宣言し、gRPCハンドラのrequest/responseに最低限の型を与える。

export interface PbLabel {
  id: number;
  name: string;
}

export interface PbTimestamp {
  seconds: number;
  nanos: number;
}

export interface PbTask {
  id: number;
  name: string;
  description?: string;
  status: string;
  finished_on: string;
  labels: PbLabel[];
  created_at: PbTimestamp;
  updated_at: PbTimestamp;
}

export interface ListTasksRequest {
  name?: string;
  status?: string;
  label_ids?: number[];
  cursor?: number;
  limit?: number;
}

export interface ListTasksResponse {
  tasks: PbTask[];
  next_cursor: number;
}

export interface GetTaskRequest {
  id: number;
}

export interface CreateTaskRequest {
  name: string;
  description?: string;
  status: string;
  finished_on: string;
  label_ids?: number[];
}

export interface UpdateTaskRequest {
  id: number;
  name: string;
  description?: string;
  status: string;
  finished_on: string;
  label_ids?: number[];
}

export interface DeleteTaskRequest {
  id: number;
}

export type DeleteTaskResponse = Record<string, never>;
