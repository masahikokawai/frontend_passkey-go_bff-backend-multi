import { apiFetch } from "@/shared/api/client";
import type { Task, TaskInput, TaskListResponse } from "./types";

const BASE = "/api/tasks";

export interface FetchTasksParams {
  limit?: number;
  offset?: number;
}

// GET /api/tasks
// BFF内部で bff.tasks-backend-v2 フラグにより backend v1(REST)/v2(gRPC)へ
// ルーティングされるが、フロントから見えるレスポンス形状は常に同じ(CONTRACT.md セクション3)
export async function fetchTasks(params: FetchTasksParams = {}): Promise<TaskListResponse> {
  const query = new URLSearchParams();
  if (params.limit !== undefined) query.set("limit", String(params.limit));
  if (params.offset !== undefined) query.set("offset", String(params.offset));
  const qs = query.toString();
  return apiFetch<TaskListResponse>(`${BASE}${qs ? `?${qs}` : ""}`);
}

export async function fetchTask(id: number): Promise<Task> {
  return apiFetch<Task>(`${BASE}/${id}`);
}

export async function createTask(input: TaskInput): Promise<Task> {
  return apiFetch<Task>(BASE, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export async function updateTask(id: number, input: TaskInput): Promise<Task> {
  return apiFetch<Task>(`${BASE}/${id}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  });
}

export async function deleteTask(id: number): Promise<void> {
  await apiFetch<void>(`${BASE}/${id}`, { method: "DELETE" });
}
