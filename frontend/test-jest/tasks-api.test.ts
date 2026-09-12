import { fetchTasks, fetchTask, createTask, updateTask, deleteTask } from "../src/features/tasks/api";
import { apiFetch } from "../src/shared/api/client";
import type { TaskInput } from "../src/features/tasks/types";

// src/features/tasks/api.test.ts (Vitest版)と同じ内容のJest版
jest.mock("../src/shared/api/client", () => ({
  __esModule: true,
  apiFetch: jest.fn(),
}));

describe("tasks/api", () => {
  it("fetchTasks: limit/offset未指定ならクエリ文字列を付けない", async () => {
    (apiFetch as jest.Mock).mockResolvedValue({ tasks: [], total: 0, limit: 20, offset: 0 });
    await fetchTasks();
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks");
  });

  it("fetchTasks: limit/offset指定時は正しいクエリ文字列を組み立てる", async () => {
    (apiFetch as jest.Mock).mockResolvedValue({ tasks: [], total: 0, limit: 5, offset: 10 });
    await fetchTasks({ limit: 5, offset: 10 });
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks?limit=5&offset=10");
  });

  it("fetchTask: idをパスに含めてGETする", async () => {
    (apiFetch as jest.Mock).mockResolvedValue({});
    await fetchTask(42);
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks/42");
  });

  it("createTask: POSTでJSON化した入力をボディに送る", async () => {
    (apiFetch as jest.Mock).mockResolvedValue({});
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
    (apiFetch as jest.Mock).mockResolvedValue({});
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
    (apiFetch as jest.Mock).mockResolvedValue(undefined);
    await deleteTask(9);
    expect(apiFetch).toHaveBeenCalledWith("/api/tasks/9", { method: "DELETE" });
  });
});
