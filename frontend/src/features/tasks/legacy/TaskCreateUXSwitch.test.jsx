import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import TaskCreateUXSwitch from "./TaskCreateUXSwitch";

vi.mock("./TaskList", () => ({
  default: () => <div data-testid="inline-marker" />,
}));
vi.mock("./TaskListModal", () => ({
  default: () => <div data-testid="modal-marker" />,
}));
vi.mock("./TaskListPage", () => ({
  default: () => <div data-testid="page-marker" />,
}));

function renderWithFlag(uxValue) {
  const featureFlags = uxValue === undefined ? {} : { "frontend.task-create-ux": uxValue };
  return render(
    <MemoryRouter initialEntries={["/parent/child"]}>
      <Routes>
        <Route path="/parent" element={<Outlet context={{ featureFlags }} />}>
          <Route path="child" element={<TaskCreateUXSwitch />} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe("legacy/TaskCreateUXSwitch", () => {
  it('frontend.task-create-ux="inline"なら常設フォーム版(legacy/TaskList.jsx)が描画される', () => {
    renderWithFlag("inline");
    expect(screen.getByTestId("inline-marker")).toBeInTheDocument();
  });

  it('frontend.task-create-ux="modal"ならモーダル版(legacy/TaskListModal.jsx)が描画される', () => {
    renderWithFlag("modal");
    expect(screen.getByTestId("modal-marker")).toBeInTheDocument();
  });

  it('frontend.task-create-ux="page"なら別ページ版(legacy/TaskListPage.jsx)が描画される', () => {
    renderWithFlag("page");
    expect(screen.getByTestId("page-marker")).toBeInTheDocument();
  });

  it("フラグ未反映(値が無い)場合は既定のinline版にフォールバックする", () => {
    renderWithFlag(undefined);
    expect(screen.getByTestId("inline-marker")).toBeInTheDocument();
  });

  it("未知の値の場合もinline版にフォールバックする", () => {
    renderWithFlag("unknown-variant");
    expect(screen.getByTestId("inline-marker")).toBeInTheDocument();
  });

  it("切り替え結果をconsole.infoに出す", () => {
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    renderWithFlag("modal");
    expect(infoSpy).toHaveBeenCalledWith(expect.stringContaining("frontend.task-create-ux=modal"));
    infoSpy.mockRestore();
  });
});
