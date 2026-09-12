import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskListPage from "./TaskListPage";
import { fetchTasks, deleteTask } from "./api";
import type { Task } from "./types";

vi.mock("./api", () => ({
  fetchTasks: vi.fn(),
  deleteTask: vi.fn(),
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
    vi.mocked(fetchTasks).mockResolvedValue({ tasks: [sampleTask], total: 1, limit: 20, offset: 0 });
    vi.mocked(deleteTask).mockResolvedValue(undefined);
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
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<TaskListPage />);
    await screen.findByText("既存タスク");

    vi.mocked(fetchTasks).mockResolvedValueOnce({ tasks: [], total: 0, limit: 20, offset: 0 });
    await userEvent.click(screen.getByTestId("task-delete-button"));

    await waitFor(() => expect(deleteTask).toHaveBeenCalledWith(1));
    expect(await screen.findByTestId("success-message")).toHaveTextContent("タスクを削除しました");
  });

  // 【テスト監査で発見・修正、TaskList.test.tsx/TaskListModal.test.tsxと同じ理由】
  // 1件目の削除ボタン押下後、deleteTask()自体が解決するまでreload()(loading状態、
  // 画面全体が「読み込み中...」に置き換わる)には到達しないため、その間は画面が
  // 操作可能なままで、2件目の削除を先に完了させることができる。この場合reload()は
  // 「2件目の削除」側が先に呼ばれ「1件目の削除」側が後から呼ばれるため、1件目側(最新)の
  // 応答が先に返り、2件目側(古い)の応答がその後に返ると、新しい状態が古い状態で
  // 上書きされてしまうバグがあった(TaskListPage.tsxのlatestRequestIdRefによる修正の回帰テスト)
  it("1件目の削除の完了待ち中に2件目の削除が先に完了しても、1件目の削除が最終的に完了した後の一覧が、2件目由来の古い応答で上書きされない", async () => {
    const secondTask: Task = { ...sampleTask, id: 2, name: "2件目のタスク" };
    vi.mocked(fetchTasks).mockResolvedValue({ tasks: [sampleTask, secondTask], total: 2, limit: 20, offset: 0 });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<TaskListPage />);
    await screen.findByText("既存タスク");
    await screen.findByText("2件目のタスク");

    // 1件目のdeleteTask自体を保留にする(reload()呼び出しより前の時点で止める)
    let resolveFirstDeleteTask!: () => void;
    vi.mocked(deleteTask).mockReturnValueOnce(
      new Promise<void>((resolve) => {
        resolveFirstDeleteTask = resolve;
      })
    );

    type TasksResponse = { tasks: Task[]; total: number; limit: number; offset: number };
    let resolveSecondDeleteReload!: (v: TasksResponse) => void;
    let resolveFirstDeleteReload!: (v: TasksResponse) => void;
    vi.mocked(fetchTasks)
      .mockReturnValueOnce(
        new Promise<TasksResponse>((resolve) => {
          resolveSecondDeleteReload = resolve;
        })
      )
      .mockReturnValueOnce(
        new Promise<TasksResponse>((resolve) => {
          resolveFirstDeleteReload = resolve;
        })
      );

    const deleteButtons = screen.getAllByTestId("task-delete-button");
    await userEvent.click(deleteButtons[0]);
    expect(deleteTask).toHaveBeenCalledWith(1);

    // 1件目の削除完了を待たずに、2件目を削除する(reload()その1、fetchTasksの2回目の呼び出し)
    await userEvent.click(deleteButtons[1]);
    await waitFor(() => expect(deleteTask).toHaveBeenCalledWith(2));

    // ここでようやく1件目の削除が完了し、そのreload()が呼ばれる(fetchTasksの3回目の呼び出し=最新のreload())
    resolveFirstDeleteTask();
    await waitFor(() => expect(fetchTasks).toHaveBeenCalledTimes(3));

    // 後発(1件目削除側、最新)のreload()応答が先に返ってくる: 2件とも削除済みのはず
    resolveFirstDeleteReload({ tasks: [], total: 0, limit: 20, offset: 0 });
    await waitFor(() => expect(screen.queryByTestId("task-row")).not.toBeInTheDocument());

    // 先発(2件目削除側、古い)のreload()応答が、1件目側より遅れて返ってくる
    // 修正前はこれが一覧を上書きし、削除したはずの「2件目のタスク」が復活して見えていた
    resolveSecondDeleteReload({ tasks: [secondTask], total: 1, limit: 20, offset: 0 });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByTestId("task-row")).not.toBeInTheDocument();
  });

  it("一覧取得が失敗した場合、エラーメッセージが表示される", async () => {
    vi.mocked(fetchTasks).mockRejectedValueOnce(new Error("一覧を取得できません"));
    render(<TaskListPage />);
    expect(await screen.findByTestId("load-error-message")).toHaveTextContent("一覧を取得できません");
  });

  it("削除ボタン押下→確認キャンセルでdeleteTaskは呼ばれない", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<TaskListPage />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(deleteTask).not.toHaveBeenCalled();
  });

  it("削除が失敗した場合、エラーメッセージが表示される", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.mocked(deleteTask).mockRejectedValueOnce(new Error("削除できません"));
    render(<TaskListPage />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(await screen.findByTestId("delete-error-message")).toHaveTextContent("削除できません");
  });
});
