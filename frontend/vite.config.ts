import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

// BFF(bff-gin/bff)は同一オリジンではなく別サービスとして起動するため、開発時は /api へのリクエストをBFFへプロキシする
// これによりブラウザ視点では同一オリジンとなり、Cookie(SameSite=Lax)が問題なく送受信できる
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  server: {
    host: true,
    port: 5173,
    proxy: {
      "/api": {
        target: process.env.VITE_BFF_ORIGIN ?? "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
