// Task関連の型定義
// BFFのJSONレスポンス形状(CONTRACT.md セクション3)と一致させる
// backendのGo構造体(model.Task)がstatusをenum型で持つのに対応し、
// ここではUnion型でRailsのenumに近い表現をする

export type TaskStatus = "waiting" | "work_in_progress" | "completed";

export interface Label {
  id: number;
  name: string;
}

export interface Task {
  id: number;
  name: string;
  description: string | null;
  status: TaskStatus;
  finishedOn: string; // "YYYY-MM-DD"
  labels: Label[];
  createdAt: string; // RFC3339
  updatedAt: string; // RFC3339
}

export interface TaskListResponse {
  tasks: Task[];
  total: number;
  limit: number;
  offset: number;
}

export interface TaskInput {
  name: string;
  description: string | null;
  status: TaskStatus;
  finishedOn: string;
  labelIds: number[];
}
