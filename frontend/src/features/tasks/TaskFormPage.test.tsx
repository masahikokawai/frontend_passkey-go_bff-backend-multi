import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import TaskFormPage from "./TaskFormPage";
import { createTask, fetchTask, updateTask } from "./api";
import { apiFetch } from "@/shared/api/client";
import type { Task } from "./types";

vi.mock("./LabelSelect", () => ({
  default: () => <div data-testid="label-select-stub" />,
}));
vi.mock("./api", () => ({
  createTask: vi.fn(),
  fetchTask: vi.fn(),
  updateTask: vi.fn(),
}));
vi.mock("@/shared/api/client", () => ({
  apiFetch: vi.fn(),
}));

const sampleTask: Task = {
  id: 1,
  name: "既存タスク",
  description: null,
  status: "waiting",
  finishedOn: "2030-01-01",
  labels: [],
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/tasks" element={<div data-testid="tasks-list-marker" />} />
        <Route path="/tasks/new" element={<TaskFormPage mode="create" />} />
        <Route path="/tasks/:id/edit" element={<TaskFormPage mode="edit" />} />
      </Routes>
    </MemoryRouter>
  );
}

describe("TaskFormPage", () => {
  beforeEach(() => {
    vi.mocked(apiFetch).mockResolvedValue({ labels: [] });
    vi.mocked(createTask).mockResolvedValue(sampleTask);
    vi.mocked(updateTask).mockResolvedValue(sampleTask);
    vi.mocked(fetchTask).mockResolvedValue(sampleTask);
  });

  it("一覧に戻るリンクが常に表示される(戻る導線の確保)", async () => {
    renderAt("/tasks/new");
    expect(await screen.findByTestId("back-to-tasks-link")).toHaveAttribute("href", "/tasks");
  });

  it("作成モード: 空のフォームが表示され、送信するとcreateTaskが呼ばれ一覧へ遷移する", async () => {
    renderAt("/tasks/new");
    await screen.findByTestId("task-form-page");

    expect(screen.getByTestId("task-name-input")).toHaveValue("");
    await userEvent.type(screen.getByTestId("task-name-input"), "新規タスク");
    await userEvent.type(screen.getByTestId("task-finished-on-input"), "2030-02-01");
    await userEvent.click(screen.getByTestId("task-submit-button"));

    await waitFor(() => expect(createTask).toHaveBeenCalledTimes(1));
    expect(await screen.findByTestId("tasks-list-marker")).toBeInTheDocument();
  });

  it("編集モード: fetchTaskで取得した値がフォームに反映される", async () => {
    renderAt("/tasks/1/edit");

    expect(await screen.findByTestId("task-name-input")).toHaveValue("既存タスク");
    expect(fetchTask).toHaveBeenCalledWith(1);
    expect(screen.getByTestId("task-submit-button")).toHaveTextContent("更新");
  });

  it("編集モード: 送信するとupdateTaskが呼ばれ一覧へ遷移する", async () => {
    renderAt("/tasks/1/edit");
    await screen.findByTestId("task-name-input");

    await userEvent.click(screen.getByTestId("task-submit-button"));

    await waitFor(() => expect(updateTask).toHaveBeenCalledWith(1, expect.anything()));
    expect(await screen.findByTestId("tasks-list-marker")).toBeInTheDocument();
  });

  it("編集モード: タスク取得が失敗した場合エラーメッセージが表示され、フォーム(送信ボタン)は表示されない", async () => {
    // 【テスト監査で追加】ブックマーク経由・別セッションでの削除後の再読み込み等で
    // 起こりうる「編集対象が既に存在しない」ケース。エラーメッセージが出るだけでなく、
    // taskがundefinedのままフォームが描画されない(=空の状態で更新を送信できてしまう
    // 見た目上壊れたフォームにならない)ことも合わせて確認する
    vi.mocked(fetchTask).mockRejectedValueOnce(new Error("タスクが見つかりません"));
    renderAt("/tasks/999/edit");

    expect(await screen.findByTestId("load-error-message")).toHaveTextContent(
      "タスクが見つかりません"
    );
    expect(screen.queryByTestId("task-submit-button")).not.toBeInTheDocument();
    // 「一覧に戻る」導線は取得失敗時も引き続き表示され続ける
    expect(screen.getByTestId("back-to-tasks-link")).toHaveAttribute("href", "/tasks");
  });

  // 【テスト監査で追加】URLの:idパラメータは信頼できない入力(ブラウザで直接書き換え可能)。
  // Number("abc")はエラーを投げずNaNを返すため、TaskFormPage側で追加の防御コードが
  // 無くてもクラッシュはしない設計だが、その性質を明示的な回帰テストとして固定する
  // (fetchTaskへNaNがそのまま渡り、それ以降は通常の取得失敗パスと同じ扱いになる)
  it("編集モードで:idが数値でないURL(/tasks/abc/edit)でも、クラッシュせず取得失敗として扱われる", async () => {
    vi.mocked(fetchTask).mockRejectedValueOnce(new Error("タスクが見つかりません"));
    renderAt("/tasks/abc/edit");

    expect(await screen.findByTestId("load-error-message")).toHaveTextContent(
      "タスクが見つかりません"
    );
    expect(fetchTask).toHaveBeenCalledWith(NaN);
    expect(screen.queryByTestId("task-submit-button")).not.toBeInTheDocument();
  });

  it("作成が失敗した場合、TaskForm自身のエラーが表示され、一覧へは遷移しない(navigate()に到達しない)", async () => {
    vi.mocked(createTask).mockRejectedValueOnce(new Error("作成できません"));
    renderAt("/tasks/new");
    await screen.findByTestId("task-form-page");

    await userEvent.type(screen.getByTestId("task-name-input"), "新規タスク");
    await userEvent.type(screen.getByTestId("task-finished-on-input"), "2030-02-01");
    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(await screen.findByText("作成できません")).toBeInTheDocument();
    expect(screen.queryByTestId("tasks-list-marker")).not.toBeInTheDocument();
    expect(screen.getByTestId("task-form-page")).toBeInTheDocument();
  });

  it("更新が失敗した場合、TaskForm自身のエラーが表示され、一覧へは遷移しない", async () => {
    vi.mocked(updateTask).mockRejectedValueOnce(new Error("更新できません"));
    renderAt("/tasks/1/edit");
    await screen.findByTestId("task-name-input");

    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(await screen.findByText("更新できません")).toBeInTheDocument();
    expect(screen.queryByTestId("tasks-list-marker")).not.toBeInTheDocument();
  });
});
