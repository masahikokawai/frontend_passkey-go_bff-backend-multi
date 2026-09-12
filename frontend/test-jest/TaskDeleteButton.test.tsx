import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskDeleteButton from "../src/features/tasks/TaskDeleteButton";

// src/features/tasks/TaskDeleteButton.test.tsx (Vitest版)と同じ内容のJest版
// vi.fn/vi.spyOn ⇔ jest.fn/jest.spyOn以外はほぼ同じ書き味であることが分かる
describe("TaskDeleteButton", () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("確認ダイアログでOKした場合のみonDeleteを呼ぶ", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(true);
    const onDelete = jest.fn().mockResolvedValue(undefined);
    render(<TaskDeleteButton onDelete={onDelete} />);

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(window.confirm).toHaveBeenCalledWith("このタスクを削除しますか?");
    expect(onDelete).toHaveBeenCalledTimes(1);
  });

  it("確認ダイアログでキャンセルした場合はonDeleteを呼ばない", async () => {
    jest.spyOn(window, "confirm").mockReturnValue(false);
    const onDelete = jest.fn().mockResolvedValue(undefined);
    render(<TaskDeleteButton onDelete={onDelete} />);

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(onDelete).not.toHaveBeenCalled();
  });
});
