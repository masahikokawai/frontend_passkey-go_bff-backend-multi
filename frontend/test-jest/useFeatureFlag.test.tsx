import { render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import type { ReactNode } from "react";
import { useFeatureFlag, useStringFeatureFlag } from "../src/shared/hooks/useFeatureFlag";

// src/shared/hooks/useFeatureFlag.test.tsx (Vitest版)と同じ内容のJest版
function Probe({ flagKey, defaultValue }: { flagKey: string; defaultValue: boolean }) {
  const value = useFeatureFlag(flagKey, defaultValue);
  return <div data-testid="probe">{String(value)}</div>;
}

function StringProbe({ flagKey, defaultValue }: { flagKey: string; defaultValue: string }) {
  const value = useStringFeatureFlag(flagKey, defaultValue);
  return <div data-testid="string-probe">{value}</div>;
}

function renderWithOutletContext(featureFlags: Record<string, boolean | string>, ui: ReactNode) {
  return render(
    <MemoryRouter initialEntries={["/parent/child"]}>
      <Routes>
        <Route path="/parent" element={<Outlet context={{ featureFlags }} />}>
          <Route path="child" element={<>{ui}</>} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe("useFeatureFlag", () => {
  it("/api/meのfeature_flagsにキーがあればその値を返す", () => {
    renderWithOutletContext(
      { "frontend.tasks-ts-rewrite": true },
      <Probe flagKey="frontend.tasks-ts-rewrite" defaultValue={false} />
    );
    expect(screen.getByTestId("probe")).toHaveTextContent("true");
  });

  it("キーが存在しない場合はdefaultValueを返す", () => {
    renderWithOutletContext({}, <Probe flagKey="unknown.flag" defaultValue={false} />);
    expect(screen.getByTestId("probe")).toHaveTextContent("false");
  });
});

describe("useStringFeatureFlag", () => {
  it("/api/meのfeature_flagsにキーがあればその値(文字列)を返す", () => {
    renderWithOutletContext(
      { "frontend.task-create-ux": "modal" },
      <StringProbe flagKey="frontend.task-create-ux" defaultValue="inline" />
    );
    expect(screen.getByTestId("string-probe")).toHaveTextContent("modal");
  });

  it("キーが存在しない場合はdefaultValueを返す", () => {
    renderWithOutletContext({}, <StringProbe flagKey="unknown.flag" defaultValue="inline" />);
    expect(screen.getByTestId("string-probe")).toHaveTextContent("inline");
  });

  it("値の型がbooleanの場合(既存フラグとの取り違え)もdefaultValueへ安全にフォールバックする", () => {
    renderWithOutletContext(
      { "frontend.tasks-ts-rewrite": true },
      <StringProbe flagKey="frontend.tasks-ts-rewrite" defaultValue="inline" />
    );
    expect(screen.getByTestId("string-probe")).toHaveTextContent("inline");
  });
});
