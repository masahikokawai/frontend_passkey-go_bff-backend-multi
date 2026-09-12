import { describe, it, expect, vi } from "vitest";
import { fetchTasks, fetchTask, createTask, updateTask, deleteTask } from "./api";
import { apiFetch } from "@/shared/api/client";
import type { TaskInput } from "./types";

// api.tsはコンポーネントのテスト(TaskList.test.tsx等)では常にvi.mockで丸ごと
// 差し替えられているため、実装本体(URL組み立て・クエリ文字列・HTTPメソッド・
// JSONボディの中身)がどのテストからも一度も実行されていなかった(厳密なレビューで判明)
//
// apiFetch自体はclient.test.tsで別途検証済みのため、
// ここではapiFetchをモックし、api.tsが「正しい引数でapiFetchを呼んでいるか」だけを検証する
vi.mock("@/shared/api/client", () => ({
  apiFetch: vi.fn(),
}));

describe("tasks/api", () => {
  it("fetchTasks: limit/offset未指定ならクエリ文字列を付けない", async () => {
    vi.mocked(apiFetch).mockResolvedValue({ tasks: [], total: 0, limit: 20, offset: 0 });
    await fetchTasks();
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks");
  });

  it("fetchTasks: limit/offset指定時は正しいクエリ文字列を組み立てる", async () => {
    vi.mocked(apiFetch).mockResolvedValue({ tasks: [], total: 0, limit: 5, offset: 10 });
    await fetchTasks({ limit: 5, offset: 10 });
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks?limit=5&offset=10");
  });

  it("fetchTask: idをパスに含めてGETする", async () => {
    vi.mocked(apiFetch).mockResolvedValue({});
    await fetchTask(42);
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks/42");
  });

  it("createTask: POSTでJSON化した入力をボディに送る", async () => {
    vi.mocked(apiFetch).mockResolvedValue({});
    const input: TaskInput = {
      name: "牛乳",
      description: null,
      status: "waiting",
      finishedOn: "2030-01-01",
      labelIds: [1, 2],
    };
    await createTask(input);
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks", {
      method: "POST",
      body: JSON.stringify(input),
    });
  });

  it("updateTask: idをパスに含めPATCHでJSON化した入力をボディに送る", async () => {
    vi.mocked(apiFetch).mockResolvedValue({});
    const input: TaskInput = {
      name: "更新後",
      description: null,
      status: "completed",
      finishedOn: "2030-02-01",
      labelIds: [],
    };
    await updateTask(7, input);
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks/7", {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  });

  it("deleteTask: idをパスに含めDELETEを送る", async () => {
    vi.mocked(apiFetch).mockResolvedValue(undefined);
    await deleteTask(9);
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks/9", { method: "DELETE" });
  });
});
