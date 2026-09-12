import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import TaskFormPage from "../../src/features/tasks/legacy/TaskFormPage";
import { createTask, fetchTask, updateTask } from "../../src/features/tasks/api";
import { apiFetch } from "../../src/shared/api/client";

// src/features/tasks/legacy/TaskFormPage.test.jsx (Vitest版)と同じ内容のJest版
jest.mock("../../src/features/tasks/LabelSelect", () => ({
  __esModule: true,
  default: () => <div data-testid="label-select-stub" />,
}));
jest.mock("../../src/features/tasks/api", () => ({
  __esModule: true,
  createTask: jest.fn(),
  fetchTask: jest.fn(),
  updateTask: jest.fn(),
}));
jest.mock("../../src/shared/api/client", () => ({
  __esModule: true,
  apiFetch: jest.fn(),
}));

const sampleTask = {
  id: 1,
  name: "既存タスク",
  description: null,
  status: "waiting",
  finishedOn: "2030-01-01",
  labels: [],
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function renderAt(path) {
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

describe("legacy/TaskFormPage", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    apiFetch.mockResolvedValue({ labels: [] });
    createTask.mockResolvedValue(sampleTask);
    updateTask.mockResolvedValue(sampleTask);
    fetchTask.mockResolvedValue(sampleTask);
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

  it("編集モード: タスク取得が失敗した場合エラーメッセージが表示される", async () => {
    fetchTask.mockRejectedValueOnce(new Error("タスクが見つかりません"));
    renderAt("/tasks/999/edit");

    expect(await screen.findByTestId("load-error-message")).toHaveTextContent(
      "タスクが見つかりません"
    );
  });

  it("作成が失敗した場合、TaskForm自身のエラーが表示され、一覧へは遷移しない(navigate()に到達しない)", async () => {
    createTask.mockRejectedValueOnce(new Error("作成できません"));
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
    updateTask.mockRejectedValueOnce(new Error("更新できません"));
    renderAt("/tasks/1/edit");
    await screen.findByTestId("task-name-input");

    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(await screen.findByText("更新できません")).toBeInTheDocument();
    expect(screen.queryByTestId("tasks-list-marker")).not.toBeInTheDocument();
  });
});
