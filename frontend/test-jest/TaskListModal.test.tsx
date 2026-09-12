import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskListModal from "../src/features/tasks/TaskListModal";
import { fetchTasks, createTask, updateTask, deleteTask } from "../src/features/tasks/api";
import { apiFetch } from "../src/shared/api/client";
import type { Task } from "../src/features/tasks/types";

// src/features/tasks/TaskListModal.test.tsx (Vitest版)と同じ内容のJest版
jest.mock("../src/features/tasks/LabelSelect", () => ({
  __esModule: true,
  default: () => <div data-testid="label-select-stub" />,
}));
jest.mock("../src/features/tasks/api", () => ({
  __esModule: true,
  fetchTasks: jest.fn(),
  createTask: jest.fn(),
  updateTask: jest.fn(),
  deleteTask: jest.fn(),
}));
jest.mock("../src/shared/api/client", () => ({
  __esModule: true,
  apiFetch: jest.fn(),
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

describe("TaskListModal(モーダル版UX)", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    (fetchTasks as jest.Mock).mockResolvedValue({ tasks: [sampleTask], total: 1, limit: 20, offset: 0 });
    (apiFetch as jest.Mock).mockResolvedValue({ labels: [{ id: 1, name: "重要" }] });
    (createTask as jest.Mock).mockResolvedValue(sampleTask);
    (updateTask as jest.Mock).mockResolvedValue(sampleTask);
    (deleteTask as jest.Mock).mockResolvedValue(undefined);
  });

  it("初期状態ではモーダルが表示されない", async () => {
    render(<TaskListModal />);
    await screen.findByText("既存タスク");
    expect(screen.queryByTestId("task-create-modal")).not.toBeInTheDocument();
  });

  it("「タスクを登録」ボタンでモーダルが開き、空のフォームが表示される", async () => {
    render(<TaskListModal />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-create-button"));

    expect(screen.getByTestId("task-create-modal")).toBeInTheDocument();
    expect(screen.getByTestId("task-name-input")).toHaveValue("");
    expect(screen.getByTestId("task-submit-button")).toHaveTextContent("作成");
  });

  it("行の「編集」ボタンでモーダルが開き、既存値が入力済みで表示される", async () => {
    render(<TaskListModal />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-edit-button"));

    expect(screen.getByTestId("task-create-modal")).toBeInTheDocument();
    expect(screen.getByTestId("task-name-input")).toHaveValue("既存タスク");
    expect(screen.getByTestId("task-submit-button")).toHaveTextContent("更新");
  });

  it("閉じるボタンでモーダルが閉じる", async () => {
    render(<TaskListModal />);
    await screen.findByText("既存タスク");
    await userEvent.click(screen.getByTestId("task-create-button"));

    await userEvent.click(screen.getByTestId("task-modal-close"));

    expect(screen.queryByTestId("task-create-modal")).not.toBeInTheDocument();
  });

  it("作成成功でモーダルが閉じ、一覧に成功メッセージが表示される", async () => {
    render(<TaskListModal />);
    await screen.findByText("既存タスク");
    await userEvent.click(screen.getByTestId("task-create-button"));

    await userEvent.type(screen.getByTestId("task-name-input"), "新規タスク");
    await userEvent.type(screen.getByTestId("task-finished-on-input"), "2030-02-01");
    await userEvent.click(screen.getByTestId("task-submit-button"));

    await waitFor(() => expect(createTask).toHaveBeenCalledTimes(1));
    expect(screen.queryByTestId("task-create-modal")).not.toBeInTheDocument();
    expect(await screen.findByTestId("success-message")).toHaveTextContent("タスクを作成しました");
  });

  it("更新成功でモーダルが閉じ、一覧に「更新しました」の成功メッセージが表示される", async () => {
    render(<TaskListModal />);
    await screen.findByText("既存タスク");
    await userEvent.click(screen.getByTestId("task-edit-button"));

    await userEvent.click(screen.getByTestId("task-submit-button"));

    await waitFor(() => expect(updateTask).toHaveBeenCalledWith(1, expect.anything()));
    expect(screen.queryByTestId("task-create-modal")).not.toBeInTheDocument();
    expect(await screen.findByTestId("success-message")).toHaveTextContent("タスクを更新しました");
  });

  it("作成が失敗した場合、モーダルは閉じずTaskForm自身のエラー表示が出る(closeModal()に到達しない)", async () => {
    (createTask as jest.Mock).mockRejectedValueOnce(new Error("作成できません"));
    render(<TaskListModal />);
    await screen.findByText("既存タスク");
    await userEvent.click(screen.getByTestId("task-create-button"));

    await userEvent.type(screen.getByTestId("task-name-input"), "新規タスク");
    await userEvent.type(screen.getByTestId("task-finished-on-input"), "2030-02-01");
    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(await screen.findByText("作成できません")).toBeInTheDocument();
    expect(screen.getByTestId("task-create-modal")).toBeInTheDocument();
  });

  it("削除ボタン押下→確認キャンセルでdeleteTaskは呼ばれず、モーダルの状態にも影響しない", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(false);
    render(<TaskListModal />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(deleteTask).not.toHaveBeenCalled();
    expect(screen.queryByTestId("task-create-modal")).not.toBeInTheDocument();
  });

  it("削除が失敗した場合、一覧にエラーメッセージが表示される", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(true);
    (deleteTask as jest.Mock).mockRejectedValueOnce(new Error("削除できません"));
    render(<TaskListModal />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(await screen.findByTestId("delete-error-message")).toHaveTextContent("削除できません");
  });

  it("一覧取得が失敗した場合、エラーメッセージが表示される(読み込み中のまま固まらない)", async () => {
    (fetchTasks as jest.Mock).mockRejectedValueOnce(new Error("一覧を取得できません"));
    render(<TaskListModal />);

    expect(await screen.findByTestId("load-error-message")).toHaveTextContent("一覧を取得できません");
  });
});
