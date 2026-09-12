import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskList from "../src/features/tasks/TaskList";
import { fetchTasks, createTask, updateTask, deleteTask } from "../src/features/tasks/api";
import { apiFetch } from "../src/shared/api/client";
import type { Task } from "../src/features/tasks/types";

// src/features/tasks/TaskList.test.tsx (Vitest版)と同じ内容のJest版
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

describe("TaskList(新実装)", () => {
  beforeEach(() => {
    // 呼び出し回数を各テストごとにリセットする(しないと前のテストのrenderで
    // 発生したfetchTasks呼び出しが蓄積し、toHaveBeenCalledTimesが食い違う)
    jest.clearAllMocks();
    (fetchTasks as jest.Mock).mockResolvedValue({ tasks: [sampleTask], total: 1, limit: 20, offset: 0 });
    (apiFetch as jest.Mock).mockResolvedValue({ labels: [{ id: 1, name: "重要" }] });
    (createTask as jest.Mock).mockResolvedValue(sampleTask);
    (updateTask as jest.Mock).mockResolvedValue(sampleTask);
    (deleteTask as jest.Mock).mockResolvedValue(undefined);
  });

  it("マウント時にfetchTasksと/api/labelsを呼び、一覧・ラベル選択肢が表示される", async () => {
    render(<TaskList />);

    expect(await screen.findByText("既存タスク")).toBeInTheDocument();
    expect(fetchTasks).toHaveBeenCalledTimes(1);
    expect(apiFetch).toHaveBeenCalledWith("/api/labels");
  });

  it("行の「編集」ボタンを押すと、その行の値がフォームに反映される", async () => {
    render(<TaskList />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-edit-button"));

    expect(screen.getByTestId("task-name-input")).toHaveValue("既存タスク");
    expect(screen.getByTestId("task-finished-on-input")).toHaveValue("2030-01-01");
    expect(screen.getByTestId("task-submit-button")).toHaveTextContent("更新");
  });

  it("削除ボタン押下→確認OKでdeleteTaskが呼ばれ、一覧が再取得される", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(true);
    render(<TaskList />);
    await screen.findByText("既存タスク");

    (fetchTasks as jest.Mock).mockResolvedValueOnce({ tasks: [], total: 0, limit: 20, offset: 0 });
    await userEvent.click(screen.getByTestId("task-delete-button"));

    await waitFor(() => expect(deleteTask).toHaveBeenCalledWith(1));
    expect(fetchTasks).toHaveBeenCalledTimes(2);
  });

  it("削除が失敗した場合、エラーメッセージが表示される(未処理のPromise rejectionにならない)", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(true);
    (deleteTask as jest.Mock).mockRejectedValueOnce(new Error("削除できません"));
    render(<TaskList />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(await screen.findByTestId("delete-error-message")).toHaveTextContent("削除できません");
  });

  it("一覧取得が失敗した場合、エラーメッセージが表示される(読み込み中のまま固まらない)", async () => {
    (fetchTasks as jest.Mock).mockRejectedValueOnce(new Error("一覧を取得できません"));
    render(<TaskList />);

    expect(await screen.findByTestId("load-error-message")).toHaveTextContent("一覧を取得できません");
  });

  it("タスクが0件のとき、テーブルの見出しだけが表示され行は出ない", async () => {
    (fetchTasks as jest.Mock).mockResolvedValueOnce({ tasks: [], total: 0, limit: 20, offset: 0 });
    render(<TaskList />);

    await screen.findByText("タスク名");
    expect(screen.queryByTestId("task-row")).not.toBeInTheDocument();
  });

  it("タスク名に<script>のような文字列が含まれていても、タグとして解釈されず文字列のまま表示される(JSXの自動エスケープ)", async () => {
    (fetchTasks as jest.Mock).mockResolvedValueOnce({
      tasks: [{ ...sampleTask, name: "<script>alert(1)</script>" }],
      total: 1,
      limit: 20,
      offset: 0,
    });
    render(<TaskList />);

    expect(await screen.findByText("<script>alert(1)</script>")).toBeInTheDocument();
    expect(document.querySelectorAll("script")).toHaveLength(0);
  });
});
