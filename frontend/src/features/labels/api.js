import { apiFetch } from "@/shared/api/client";

// CONTRACT.md セクション14: /api/labels のCRUD
// featuresディレクトリの中でもlabelsはTypeScript化の対象外(段階移行、
// CONTRACT.mdセクション6)のため、tasks/api.tsと同じ書き方をJavaScriptのまま用意する
const BASE = "/api/labels";

export async function fetchLabels() {
  return apiFetch(BASE);
}

export async function createLabel(name) {
  return apiFetch(BASE, {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export async function updateLabel(id, name) {
  return apiFetch(`${BASE}/${id}`, {
    method: "PATCH",
    body: JSON.stringify({ name }),
  });
}

export async function deleteLabel(id) {
  await apiFetch(`${BASE}/${id}`, { method: "DELETE" });
}
