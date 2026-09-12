import { defineConfig } from "@playwright/test";

// FRONTEND_BASE_URL: /tasks等の画面を実際に配信するのはfrontend(Vite dev server)であり、bff(:8080)は/api/*しか持たない
// 以前は誤って"BFFが静的ファイルを配信する前提"で bff のオリジンを既定値にしており、goto("/tasks")がbffの404になっていた(実機検証で発覚)
const baseURL = process.env.FRONTEND_BASE_URL ?? "http://localhost:5173";

export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  retries: 0,
  use: {
    baseURL,
    trace: "on-first-retry",
  },
});
