import { describe, it, expect, vi } from "vitest";
import { fetchLabels, createLabel, updateLabel, deleteLabel } from "./api";
import { apiFetch } from "@/shared/api/client";

// tasks/api.test.tsと同じ理由(コンポーネントテストでは常にvi.mockで丸ごと
// 差し替えられており、実装本体が一度も実行されていなかった)で新設
vi.mock("@/shared/api/client", () => ({
  apiFetch: vi.fn(),
}));

describe("labels/api", () => {
  it("fetchLabels: GETする", async () => {
    vi.mocked(apiFetch).mockResolvedValue({ labels: [] });
    await fetchLabels();
    expect(apiFetch).toHaveBeenCalledWith("/api/labels");
  });

  it("createLabel: POSTで{name}をJSON化してボディに送る", async () => {
    vi.mocked(apiFetch).mockResolvedValue({});
    await createLabel("重要");
    expect(apiFetch).toHaveBeenCalledWith("/api/labels", {
      method: "POST",
      body: JSON.stringify({ name: "重要" }),
    });
  });

  it("updateLabel: idをパスに含めPATCHで{name}をボディに送る", async () => {
    vi.mocked(apiFetch).mockResolvedValue({});
    await updateLabel(3, "更新後");
    expect(apiFetch).toHaveBeenCalledWith("/api/labels/3", {
      method: "PATCH",
      body: JSON.stringify({ name: "更新後" }),
    });
  });

  it("deleteLabel: idをパスに含めDELETEを送る", async () => {
    vi.mocked(apiFetch).mockResolvedValue(undefined);
    await deleteLabel(5);
    expect(apiFetch).toHaveBeenCalledWith("/api/labels/5", { method: "DELETE" });
  });
});
