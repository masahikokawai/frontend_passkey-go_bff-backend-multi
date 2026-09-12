import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskList from "./TaskList";
import { fetchTasks, createTask, updateTask, deleteTask } from "../api";
import { apiFetch } from "@/shared/api/client";

// 旧実装(JavaScript)
// ../TaskList.test.tsx(新実装)と同じ観点で検証し、
// 両者の違いがTypeScript/JavaScriptという実装言語だけであることを示す位置づけ
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

describe("legacy/TaskList(旧実装)", () => {
  beforeEach(() => {
    vi.mocked(fetchTasks).mockResolvedValue({ tasks: [sampleTask], total: 1, limit: 20, offset: 0 });
    vi.mocked(apiFetch).mockResolvedValue({ labels: [{ id: 1, name: "重要" }] });
    vi.mocked(createTask).mockResolvedValue(sampleTask);
    vi.mocked(updateTask).mockResolvedValue(sampleTask);
    vi.mocked(deleteTask).mockResolvedValue(undefined);
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
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<TaskList />);
    await screen.findByText("既存タスク");

    vi.mocked(fetchTasks).mockResolvedValueOnce({ tasks: [], total: 0, limit: 20, offset: 0 });
    await userEvent.click(screen.getByTestId("task-delete-button"));

    await waitFor(() => expect(deleteTask).toHaveBeenCalledWith(1));
    expect(fetchTasks).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId("success-message")).toHaveTextContent("タスクを削除しました");
  });

  // 【テスト監査で発見・修正】TaskList.tsx(新実装)と全く同じstale-response上書きバグが
  // legacy/TaskList.jsxにもあった(reload()呼び出しごとの採番ガードが無く、削除の完了待ち中に
  // 別の更新操作を先に完了させると、後発(削除側)のreload()応答が先に届いた後、
  // 先発(更新側)の古い応答がそれを上書きしてしまう)。TaskList.test.tsxの同名テストと
  // 同じ手順で回帰確認する
  it("削除操作の完了待ち中に別の更新操作が先に完了しても、削除操作が最終的に完了した後の一覧が、更新操作由来の古い応答で上書きされない", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<TaskList />);
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

    // 削除ボタンを押す。deleteTask()がまだ解決しないため、reload()にはまだ到達しない
    await userEvent.click(screen.getByTestId("task-delete-button"));
    expect(deleteTask).toHaveBeenCalledTimes(1);

    // 削除の完了を待たずに、別の編集→更新を行う(reload()その1、fetchTasksの2回目の呼び出し)
    await userEvent.click(screen.getByTestId("task-edit-button"));
    await userEvent.click(screen.getByTestId("task-submit-button"));
    await waitFor(() => expect(updateTask).toHaveBeenCalledTimes(1));

    // ここでようやく削除が完了し、削除側のreload()が呼ばれる(fetchTasksの3回目の呼び出し=最新)
    resolveDeleteTask();
    await waitFor(() => expect(fetchTasks).toHaveBeenCalledTimes(3));

    // 後発(削除側、最新)のreload()応答が先に返ってくる
    resolveDeleteReload({ tasks: [], total: 0, limit: 20, offset: 0 });
    await waitFor(() => expect(screen.queryByTestId("task-row")).not.toBeInTheDocument());

    // 先発(更新側、古い)のreload()応答が、削除側より遅れて返ってくる
    // 修正前はこれが一覧を上書きし、削除したはずのタスクが「更新後タスク」として復活して見えていた
    resolveUpdateReload({ tasks: [{ ...sampleTask, name: "更新後タスク" }], total: 1, limit: 20, offset: 0 });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByTestId("task-row")).not.toBeInTheDocument();
  });

  it("作成成功時に「作成しました」の成功メッセージが出る", async () => {
    render(<TaskList />);
    await screen.findByText("既存タスク");

    await userEvent.type(screen.getByTestId("task-name-input"), "新しいタスク");
    fireEvent.change(screen.getByTestId("task-finished-on-input"), {
      target: { value: "2030-02-01" },
    });
    await userEvent.click(screen.getByTestId("task-submit-button"));

    await waitFor(() => expect(createTask).toHaveBeenCalledTimes(1));
    expect(screen.getByTestId("success-message")).toHaveTextContent("タスクを作成しました");
  });

  it("編集→更新送信でupdateTaskが呼ばれ、「更新しました」の成功メッセージが出る", async () => {
    render(<TaskList />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-edit-button"));
    await userEvent.click(screen.getByTestId("task-submit-button"));

    await waitFor(() => expect(updateTask).toHaveBeenCalledWith(1, expect.anything()));
    expect(screen.getByTestId("success-message")).toHaveTextContent("タスクを更新しました");
  });

  it("createTaskが失敗した場合、エラーメッセージを表示し成功メッセージは出さない", async () => {
    vi.mocked(createTask).mockRejectedValue(new Error("タスク名は20文字以内で入力してください"));
    render(<TaskList />);
    await screen.findByText("既存タスク");

    await userEvent.type(screen.getByTestId("task-name-input"), "新しいタスク");
    fireEvent.change(screen.getByTestId("task-finished-on-input"), {
      target: { value: "2030-02-01" },
    });
    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(await screen.findByText("タスク名は20文字以内で入力してください")).toBeInTheDocument();
    expect(screen.queryByTestId("success-message")).not.toBeInTheDocument();
  });

  it("削除ボタン押下→確認キャンセルでdeleteTaskは呼ばれない", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<TaskList />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(deleteTask).not.toHaveBeenCalled();
  });

  it("削除が失敗した場合、エラーメッセージが表示される(未処理のPromise rejectionにならない)", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.mocked(deleteTask).mockRejectedValueOnce(new Error("削除できません"));
    render(<TaskList />);
    await screen.findByText("既存タスク");

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(await screen.findByText("削除できません")).toBeInTheDocument();
  });

  it("一覧取得が失敗した場合、エラーメッセージが表示される(読み込み中のまま固まらない)", async () => {
    vi.mocked(fetchTasks).mockRejectedValueOnce(new Error("一覧を取得できません"));
    render(<TaskList />);

    expect(await screen.findByText("一覧を取得できません")).toBeInTheDocument();
  });

  it("タスクが0件のとき、テーブルの見出しだけが表示され行は出ない", async () => {
    vi.mocked(fetchTasks).mockResolvedValueOnce({ tasks: [], total: 0, limit: 20, offset: 0 });
    render(<TaskList />);

    await screen.findByText("タスク名");
    expect(screen.queryByTestId("task-row")).not.toBeInTheDocument();
  });

  it("タスク名に<script>のような文字列が含まれていても、タグとして解釈されず文字列のまま表示される(JSXの自動エスケープ)", async () => {
    vi.mocked(fetchTasks).mockResolvedValueOnce({
      tasks: [{ ...sampleTask, name: "<script>alert(1)</script>" }],
      total: 1,
      limit: 20,
      offset: 0,
    });
    render(<TaskList />);

    expect(await screen.findByText("<script>alert(1)</script>")).toBeInTheDocument();
    expect(document.querySelectorAll("script")).toHaveLength(0);
  });

  // 【テスト監査で発見・修正】TaskForm.test.tsxの同名テスト参照。legacy版の独自フォームJSXにも
  // 同じhtmlFor/id欠落バグがあった
  it("各入力欄がlabelから正しく参照できる(htmlFor/idの関連付け)", async () => {
    render(<TaskList />);
    await screen.findByText("既存タスク");

    expect(screen.getByLabelText("タスク名(20文字以内)")).toBe(screen.getByTestId("task-name-input"));
    expect(screen.getByLabelText("説明")).toBeInTheDocument();
    expect(screen.getByLabelText("ステータス")).toBe(screen.getByTestId("task-status-select"));
    expect(screen.getByLabelText("期限(過去日不可)")).toBe(screen.getByTestId("task-finished-on-input"));
  });
});
