import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskForm from "../src/features/tasks/TaskForm";
import type { Task } from "../src/features/tasks/types";

// src/features/tasks/TaskForm.test.tsx (Vitest版)と同じ内容のJest版
jest.mock("../src/features/tasks/LabelSelect", () => ({
  __esModule: true,
  default: () => <div data-testid="label-select-stub" />,
}));

describe("TaskForm", () => {
  it("入力した内容通りのTaskInputでonSubmitが呼ばれる(新規作成)", async () => {
    const onSubmit = jest.fn().mockResolvedValue(undefined);
    render(<TaskForm labelOptions={[]} submitLabel="作成" onSubmit={onSubmit} />);

    await userEvent.type(screen.getByTestId("task-name-input"), "牛乳を買う");
    fireEvent.change(screen.getByTestId("task-status-select"), {
      target: { value: "work_in_progress" },
    });
    fireEvent.change(screen.getByTestId("task-finished-on-input"), {
      target: { value: "2030-01-01" },
    });
    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(onSubmit).toHaveBeenCalledWith({
      name: "牛乳を買う",
      description: null,
      status: "work_in_progress",
      finishedOn: "2030-01-01",
      labelIds: [],
    });
  });

  it("タスク名inputはrequired属性を持つ(20文字以内・必須のバリデーション)", () => {
    render(<TaskForm labelOptions={[]} submitLabel="作成" onSubmit={jest.fn()} />);
    const nameInput = screen.getByTestId("task-name-input");
    expect(nameInput).toBeRequired();
    expect(nameInput).toHaveAttribute("maxlength", "20");
  });

  it("onSubmitがErrorで失敗した場合、そのmessageをalert-dangerで表示し成功メッセージは出さない", async () => {
    const onSubmit = jest.fn().mockRejectedValue(new Error("タスク名は20文字以内で入力してください"));
    render(<TaskForm labelOptions={[]} submitLabel="作成" onSubmit={onSubmit} />);

    await userEvent.type(screen.getByTestId("task-name-input"), "牛乳を買う");
    fireEvent.change(screen.getByTestId("task-finished-on-input"), { target: { value: "2030-01-01" } });
    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(await screen.findByText("タスク名は20文字以内で入力してください")).toBeInTheDocument();
    expect(screen.queryByTestId("success-message")).not.toBeInTheDocument();
    expect(screen.getByTestId("task-name-input")).toHaveValue("牛乳を買う");
  });

  it("onSubmitがError以外の値で失敗した場合、汎用メッセージ「保存に失敗しました」を表示する", async () => {
    const onSubmit = jest.fn().mockRejectedValue("some non-error rejection");
    render(<TaskForm labelOptions={[]} submitLabel="作成" onSubmit={onSubmit} />);

    await userEvent.type(screen.getByTestId("task-name-input"), "牛乳を買う");
    fireEvent.change(screen.getByTestId("task-finished-on-input"), { target: { value: "2030-01-01" } });
    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(await screen.findByText("保存に失敗しました")).toBeInTheDocument();
  });

  it("編集時(initial指定)は既存タスクの値が初期表示される", () => {
    const task: Task = {
      id: 1,
      name: "既存タスク",
      description: "メモ",
      status: "waiting",
      finishedOn: "2030-05-01",
      labels: [{ id: 1, name: "重要" }],
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
    };
    render(
      <TaskForm initial={task} labelOptions={[]} submitLabel="更新" onSubmit={jest.fn()} />
    );

    expect(screen.getByTestId("task-name-input")).toHaveValue("既存タスク");
    expect(screen.getByTestId("task-finished-on-input")).toHaveValue("2030-05-01");
    expect(screen.getByTestId("task-submit-button")).toHaveTextContent("更新");
  });

  it("送信中は送信ボタンがdisabledになり、二重送信を防ぐ", async () => {
    let resolveSubmit: () => void;
    const onSubmit = jest.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveSubmit = resolve;
        })
    );
    render(<TaskForm labelOptions={[]} submitLabel="作成" onSubmit={onSubmit} />);

    await userEvent.type(screen.getByTestId("task-name-input"), "牛乳を買う");
    fireEvent.change(screen.getByTestId("task-finished-on-input"), {
      target: { value: "2030-01-01" },
    });
    const submitButton = screen.getByTestId("task-submit-button");
    expect(submitButton).not.toBeDisabled();

    await userEvent.click(submitButton);
    await waitFor(() => expect(submitButton).toBeDisabled());

    resolveSubmit!();
    await screen.findByTestId("success-message");
    expect(submitButton).not.toBeDisabled();
  });
});
