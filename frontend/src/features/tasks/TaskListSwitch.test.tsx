import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import TaskListSwitch from "./TaskListSwitch";

// 新旧の実装本体は重いのでダミーに差し替え、「どちらが描画されたか」だけを検証する
vi.mock("./TaskList", () => ({
  default: () => <div data-testid="new-impl-marker" />,
}));
vi.mock("./legacy/TaskList", () => ({
  default: () => <div data-testid="legacy-impl-marker" />,
}));

function renderWithFlag(flagValue: boolean) {
  return render(
    <MemoryRouter initialEntries={["/parent/child"]}>
      <Routes>
        <Route
          path="/parent"
          element={
            <Outlet context={{ featureFlags: { "frontend.tasks-ts-rewrite": flagValue } }} />
          }
        >
          <Route path="child" element={<TaskListSwitch />} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe("TaskListSwitch", () => {
  it("frontend.tasks-ts-rewriteがtrueなら新実装(TaskList.tsx)が描画される", () => {
    renderWithFlag(true);
    expect(screen.getByTestId("new-impl-marker")).toBeInTheDocument();
    expect(screen.queryByTestId("legacy-impl-marker")).not.toBeInTheDocument();
  });

  it("frontend.tasks-ts-rewriteがfalseなら旧実装(legacy/TaskList.jsx)が描画される", () => {
    renderWithFlag(false);
    expect(screen.getByTestId("legacy-impl-marker")).toBeInTheDocument();
    expect(screen.queryByTestId("new-impl-marker")).not.toBeInTheDocument();
  });

  // Feature Flagの切り替えが実際にどちらの実装を表示したかブラウザのコンソールから追えるようにするためのログ(TaskListSwitch.tsx参照)
  // ログの中身自体を検証する
  it("frontend.tasks-ts-rewriteがtrueのとき、新実装を選んだ旨をconsole.infoに出す", () => {
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    renderWithFlag(true);
    expect(infoSpy).toHaveBeenCalledWith(
      expect.stringContaining("frontend.tasks-ts-rewrite=true")
    );
    expect(infoSpy).toHaveBeenCalledWith(
      expect.stringContaining("新実装(TypeScript, TaskList.tsx)")
    );
    infoSpy.mockRestore();
  });

  it("frontend.tasks-ts-rewriteがfalseのとき、旧実装を選んだ旨をconsole.infoに出す", () => {
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    renderWithFlag(false);
    expect(infoSpy).toHaveBeenCalledWith(
      expect.stringContaining("frontend.tasks-ts-rewrite=false")
    );
    expect(infoSpy).toHaveBeenCalledWith(
      expect.stringContaining("旧実装(JavaScript, legacy/TaskList.jsx)")
    );
    infoSpy.mockRestore();
  });
});
