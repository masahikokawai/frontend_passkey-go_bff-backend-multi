import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskForm from "./TaskForm";
import type { Task } from "./types";

// LabelSelectはjQuery/Select2(CDN読み込み、jsdomには実体が無い)に依存するため、
// TaskForm自体のロジック(入力→送信データの組み立て)を検証する範囲では
// スタブに差し替える(このファイルと同じ内容をtest-jest/TaskForm.test.tsxにも用意)
vi.mock("./LabelSelect", () => ({
  default: () => <div data-testid="label-select-stub" />,
}));

describe("TaskForm", () => {
  it("入力した内容通りのTaskInputでonSubmitが呼ばれる(新規作成)", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<TaskForm labelOptions={[]} submitLabel="作成" onSubmit={onSubmit} />);

    await userEvent.type(screen.getByTestId("task-name-input"), "牛乳を買う");
    // <select>/<input type="date">はfireEvent.changeで値を設定する
    // (userEvent.typeは日付inputのブラウザロケール依存の挙動と相性が悪いため)
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
    expect(screen.getByTestId("success-message")).toHaveTextContent("タスクを作成しました");
  });

  it("編集時(initial指定)の送信成功で「更新しました」の成功メッセージが出る", async () => {
    const task: Task = {
      id: 1,
      name: "既存タスク",
      description: null,
      status: "waiting",
      finishedOn: "2030-05-01",
      labels: [],
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
    };
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<TaskForm initial={task} labelOptions={[]} submitLabel="更新" onSubmit={onSubmit} />);

    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(screen.getByTestId("success-message")).toHaveTextContent("タスクを更新しました");
  });

  it("タスク名inputはrequired属性を持つ(20文字以内・必須のバリデーション)", () => {
    render(<TaskForm labelOptions={[]} submitLabel="作成" onSubmit={vi.fn()} />);
    const nameInput = screen.getByTestId("task-name-input");
    expect(nameInput).toBeRequired();
    expect(nameInput).toHaveAttribute("maxlength", "20");
  });

  it("onSubmitがErrorで失敗した場合、そのmessageをalert-dangerで表示し成功メッセージは出さない", async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error("タスク名は20文字以内で入力してください"));
    render(<TaskForm labelOptions={[]} submitLabel="作成" onSubmit={onSubmit} />);

    await userEvent.type(screen.getByTestId("task-name-input"), "牛乳を買う");
    fireEvent.change(screen.getByTestId("task-finished-on-input"), { target: { value: "2030-01-01" } });
    await userEvent.click(screen.getByTestId("task-submit-button"));

    expect(await screen.findByText("タスク名は20文字以内で入力してください")).toBeInTheDocument();
    expect(screen.queryByTestId("success-message")).not.toBeInTheDocument();
    // 失敗時はフォームの入力内容を消さない(再送信できるように)
    expect(screen.getByTestId("task-name-input")).toHaveValue("牛乳を買う");
  });

  it("onSubmitがError以外の値で失敗した場合、汎用メッセージ「保存に失敗しました」を表示する", async () => {
    const onSubmit = vi.fn().mockRejectedValue("some non-error rejection");
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
      <TaskForm initial={task} labelOptions={[]} submitLabel="更新" onSubmit={vi.fn()} />
    );

    expect(screen.getByTestId("task-name-input")).toHaveValue("既存タスク");
    expect(screen.getByTestId("task-finished-on-input")).toHaveValue("2030-05-01");
    expect(screen.getByTestId("task-submit-button")).toHaveTextContent("更新");
  });

  it("送信中は送信ボタンがdisabledになり、二重送信を防ぐ", async () => {
    let resolveSubmit: () => void;
    const onSubmit = vi.fn(
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

  // 【テスト監査で発見・修正】各<label>にhtmlFor/idの関連付けが無く、視覚的には
  // ラベルに見えても、スクリーンリーダーや「ラベルをクリックして入力欄にフォーカス」という
  // 標準的なブラウザ挙動が効いていなかった(admin/goの同種バグの修正パターンを踏襲)。
  // getByLabelTextは正しい関連付けが無いと要素を見つけられないため、これ自体が回帰確認になる
  it("各入力欄がlabelから正しく参照できる(htmlFor/idの関連付け)", () => {
    render(<TaskForm labelOptions={[]} submitLabel="作成" onSubmit={vi.fn()} />);

    expect(screen.getByLabelText("タスク名(20文字以内)")).toBe(screen.getByTestId("task-name-input"));
    expect(screen.getByLabelText("説明")).toBeInTheDocument();
    expect(screen.getByLabelText("ステータス")).toBe(screen.getByTestId("task-status-select"));
    expect(screen.getByLabelText("期限(過去日不可)")).toBe(screen.getByTestId("task-finished-on-input"));
  });
});
