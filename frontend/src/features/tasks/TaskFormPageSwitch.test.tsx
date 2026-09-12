import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import TaskFormPageSwitch from "./TaskFormPageSwitch";

// 本体は重いのでダミーに差し替え、「どちらが描画されたか」と mode の引き渡しだけを検証する
vi.mock("./TaskFormPage", () => ({
  default: ({ mode }: { mode: string }) => <div data-testid="new-impl-marker">{mode}</div>,
}));
vi.mock("./legacy/TaskFormPage", () => ({
  default: ({ mode }: { mode: string }) => <div data-testid="legacy-impl-marker">{mode}</div>,
}));

function renderWithFlag(flagValue: boolean, mode: "create" | "edit") {
  return render(
    <MemoryRouter initialEntries={["/parent/child"]}>
      <Routes>
        <Route
          path="/parent"
          element={<Outlet context={{ featureFlags: { "frontend.tasks-ts-rewrite": flagValue } }} />}
        >
          <Route path="child" element={<TaskFormPageSwitch mode={mode} />} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe("TaskFormPageSwitch", () => {
  it("frontend.tasks-ts-rewriteがtrueなら新実装(TaskFormPage.tsx)がmode付きで描画される", () => {
    renderWithFlag(true, "create");
    expect(screen.getByTestId("new-impl-marker")).toHaveTextContent("create");
    expect(screen.queryByTestId("legacy-impl-marker")).not.toBeInTheDocument();
  });

  it("frontend.tasks-ts-rewriteがfalseなら旧実装(legacy/TaskFormPage.jsx)がmode付きで描画される", () => {
    renderWithFlag(false, "edit");
    expect(screen.getByTestId("legacy-impl-marker")).toHaveTextContent("edit");
    expect(screen.queryByTestId("new-impl-marker")).not.toBeInTheDocument();
  });
});
