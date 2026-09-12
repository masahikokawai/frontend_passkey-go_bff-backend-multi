import { fetchLabels, createLabel, updateLabel, deleteLabel } from "../src/features/labels/api";
import { apiFetch } from "../src/shared/api/client";

// src/features/labels/api.test.js (Vitest版)と同じ内容のJest版
jest.mock("../src/shared/api/client", () => ({
  __esModule: true,
  apiFetch: jest.fn(),
}));

describe("labels/api", () => {
  it("fetchLabels: GETする", async () => {
    apiFetch.mockResolvedValue({ labels: [] });
    await fetchLabels();
    expect(apiFetch).toHaveBeenCalledWith("/api/labels");
  });

  it("createLabel: POSTで{name}をJSON化してボディに送る", async () => {
    apiFetch.mockResolvedValue({});
    await createLabel("重要");
    expect(apiFetch).toHaveBeenCalledWith("/api/labels", {
      method: "POST",
      body: JSON.stringify({ name: "重要" }),
    });
  });

  it("updateLabel: idをパスに含めPATCHで{name}をボディに送る", async () => {
    apiFetch.mockResolvedValue({});
    await updateLabel(3, "更新後");
    expect(apiFetch).toHaveBeenCalledWith("/api/labels/3", {
      method: "PATCH",
      body: JSON.stringify({ name: "更新後" }),
    });
  });

  it("deleteLabel: idをパスに含めDELETEを送る", async () => {
    apiFetch.mockResolvedValue(undefined);
    await deleteLabel(5);
    expect(apiFetch).toHaveBeenCalledWith("/api/labels/5", { method: "DELETE" });
  });
});
