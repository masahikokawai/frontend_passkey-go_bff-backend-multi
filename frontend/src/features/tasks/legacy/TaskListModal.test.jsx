import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskListModal from "./TaskListModal";
import { fetchTasks, createTask, updateTask, deleteTask } from "../api";
import { apiFetch } from "@/shared/api/client";

// TaskListModal.test.tsx(新実装)と同じ観点で検証する
vi.mock("../LabelSelect", () => ({
  default: () => <div data-testid="label-select-stub" />,
}));
vi.mock("../api", () => ({
  fetchTasks: vi.fn(),
  createTask: vi.fn(),
  updateTask: vi.fn(),
  deleteTask: vi.fn(),
}));
vi.mock("@/shared/api/client", () => ({
  apiFetch: vi.fn(),
}));

const sampleTask = {
  id: 1,
  name: "既存タスク",
  description: null,
  status: "waiting",
  finishedOn: "2030-01-01",
  labels: [{ id: 1, name: "重要" }],
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("legacy/TaskListModal(旧実装・モーダル版UX)", () => {
  beforeEach(() => {
    vi.mocked(fetchTasks).mockResolvedValue({ tasks: [sampleTask], total: 1, limit: 20, offset: 0 });
    vi.mocked(apiFetch).mockResolvedValue({ labels: [{ id: 1, name: "重要" }] });
    vi.mocked(createTask).mockResolvedValue(sampleTask);
    vi.mocked(updateTask).mockResolvedValue(sampleTask);
    vi.mocked(deleteTask).mockResolvedValue(undefined);
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
    await userEvent.click(screen.getByTestId("task-status-select"));
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
    vi.mocked(createTask).mockRejectedValueOnce(new Error("作成できません"));
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
    vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<TaskListModal />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(deleteTask).not.toHaveBeenCalled();
    expect(screen.queryByTestId("task-create-modal")).not.toBeInTheDocument();
  });

  it("削除が失敗した場合、一覧にエラーメッセージが表示される", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.mocked(deleteTask).mockRejectedValueOnce(new Error("削除できません"));
    render(<TaskListModal />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(await screen.findByTestId("delete-error-message")).toHaveTextContent("削除できません");
  });

  it("一覧取得が失敗した場合、エラーメッセージが表示される(読み込み中のまま固まらない)", async () => {
    vi.mocked(fetchTasks).mockRejectedValueOnce(new Error("一覧を取得できません"));
    render(<TaskListModal />);

    expect(await screen.findByTestId("load-error-message")).toHaveTextContent("一覧を取得できません");
  });

  // 【テスト監査で発見・修正】TaskListModal.tsx(新実装)と全く同じstale-response上書きバグが
  // legacy/TaskListModal.jsxにもあった(legacy/TaskList.test.jsxの同名テスト参照)
  it("削除操作の完了待ち中に別の更新操作が先に完了しても、削除操作が最終的に完了した後の一覧が、更新操作由来の古い応答で上書きされない", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<TaskListModal />);
    await screen.findByText("既存タスク");

    let resolveDeleteTask;
    vi.mocked(deleteTask).mockReturnValueOnce(
      new Promise((resolve) => {
        resolveDeleteTask = resolve;
      })
    );

    let resolveUpdateReload;
    let resolveDeleteReload;
    vi.mocked(fetchTasks)
      .mockReturnValueOnce(
        new Promise((resolve) => {
          resolveUpdateReload = resolve;
        })
      )
      .mockReturnValueOnce(
        new Promise((resolve) => {
          resolveDeleteReload = resolve;
        })
      );

    await userEvent.click(screen.getByTestId("task-delete-button"));
    expect(deleteTask).toHaveBeenCalledTimes(1);

    await userEvent.click(screen.getByTestId("task-edit-button"));
    await userEvent.click(screen.getByTestId("task-submit-button"));
    await waitFor(() => expect(updateTask).toHaveBeenCalledTimes(1));

    resolveDeleteTask();
    await waitFor(() => expect(fetchTasks).toHaveBeenCalledTimes(3));

    resolveDeleteReload({ tasks: [], total: 0, limit: 20, offset: 0 });
    await waitFor(() => expect(screen.queryByTestId("task-row")).not.toBeInTheDocument());

    // 修正前はこれが一覧を上書きし、削除したはずのタスクが「更新後タスク」として復活して見えていた
    resolveUpdateReload({ tasks: [{ ...sampleTask, name: "更新後タスク" }], total: 1, limit: 20, offset: 0 });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByTestId("task-row")).not.toBeInTheDocument();
  });
});
