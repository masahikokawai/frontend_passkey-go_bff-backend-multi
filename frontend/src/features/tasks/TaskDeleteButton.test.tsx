import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TaskDeleteButton from "./TaskDeleteButton";

// このファイルと同じ内容のテストを test-jest/TaskDeleteButton.test.tsx にJestで
// 用意している(vi.fn/vi.spyOn ⇔ jest.fn/jest.spyOnの書き味の違いを比較する副読本)
describe("TaskDeleteButton", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("確認ダイアログでOKした場合のみonDeleteを呼ぶ", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(<TaskDeleteButton onDelete={onDelete} />);

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(window.confirm).toHaveBeenCalledWith("このタスクを削除しますか?");
    expect(onDelete).toHaveBeenCalledTimes(1);
  });

  it("確認ダイアログでキャンセルした場合はonDeleteを呼ばない", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(false);
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(<TaskDeleteButton onDelete={onDelete} />);

    await userEvent.click(screen.getByTestId("task-delete-button"));

    expect(onDelete).not.toHaveBeenCalled();
  });
});
