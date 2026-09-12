import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskListPage from "../src/features/tasks/TaskListPage";
import { fetchTasks, deleteTask } from "../src/features/tasks/api";
import type { Task } from "../src/features/tasks/types";

// src/features/tasks/TaskListPage.test.tsx (Vitest版)と同じ内容のJest版
jest.mock("../src/features/tasks/api", () => ({
  __esModule: true,
  fetchTasks: jest.fn(),
  deleteTask: jest.fn(),
}));

const sampleTask: Task = {
  id: 1,
  name: "既存タスク",
  description: null,
  status: "waiting",
  finishedOn: "2030-01-01",
  labels: [{ id: 1, name: "重要" }],
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("TaskListPage(別ページ遷移版UX)", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    (fetchTasks as jest.Mock).mockResolvedValue({ tasks: [sampleTask], total: 1, limit: 20, offset: 0 });
    (deleteTask as jest.Mock).mockResolvedValue(undefined);
  });

  it("マウント時にfetchTasksを呼び、一覧が表示される", async () => {
    render(<TaskListPage />);
    expect(await screen.findByText("既存タスク")).toBeInTheDocument();
    expect(fetchTasks).toHaveBeenCalledTimes(1);
  });

  it("「タスクを登録」が/tasks/newへのリンクになっている(別画面遷移)", async () => {
    render(<TaskListPage />);
    await screen.findByText("既存タスク");
    expect(screen.getByTestId("task-create-link")).toHaveAttribute("href", "/tasks/new");
  });

  it("行の「編集」が/tasks/:id/editへのリンクになっている(別画面遷移)", async () => {
    render(<TaskListPage />);
    await screen.findByText("既存タスク");
    expect(screen.getByTestId("task-edit-button")).toHaveAttribute("href", "/tasks/1/edit");
  });

  it("削除ボタン押下→確認OKでdeleteTaskが呼ばれ、一覧が再取得される", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(true);
    render(<TaskListPage />);
    await screen.findByText("既存タスク");

    (fetchTasks as jest.Mock).mockResolvedValueOnce({ tasks: [], total: 0, limit: 20, offset: 0 });
    await userEvent.click(screen.getByTestId("task-delete-button"));

    await waitFor(() => expect(deleteTask).toHaveBeenCalledWith(1));
    expect(await screen.findByTestId("success-message")).toHaveTextContent("タスクを削除しました");
  });

  it("一覧取得が失敗した場合、エラーメッセージが表示される", async () => {
    (fetchTasks as jest.Mock).mockRejectedValueOnce(new Error("一覧を取得できません"));
    render(<TaskListPage />);
    expect(await screen.findByTestId("load-error-message")).toHaveTextContent("一覧を取得できません");
  });

  it("削除ボタン押下→確認キャンセルでdeleteTaskは呼ばれない", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(false);
    render(<TaskListPage />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(deleteTask).not.toHaveBeenCalled();
  });

  it("削除が失敗した場合、エラーメッセージが表示される", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(true);
    (deleteTask as jest.Mock).mockRejectedValueOnce(new Error("削除できません"));
    render(<TaskListPage />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(await screen.findByTestId("delete-error-message")).toHaveTextContent("削除できません");
  });
});
