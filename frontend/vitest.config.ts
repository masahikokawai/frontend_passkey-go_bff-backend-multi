import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "node:path";

// Vitestの設定
// vite.config.tsとは別ファイルにしている(Vite本体の設定に
// テスト専用の設定(environment/setupFiles)を混ぜると、本番ビルド時にも
// jsdom関連の型解決などが余計に絡むのを避けるため)
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    globals: false,
    // Jest用のテスト(test-jest/配下)はVitestの対象から除外する
    // (同じテストケースをJestとVitestで比較する副読本のため、二重実行を避ける)
    exclude: ["**/node_modules/**", "**/test-jest/**"],
    // 【テスト分離監査で追記】Vitest 5.0.0はclearMocksが既定でtrueだが、
    // このファイルに明記が無く「このプロジェクトが意図してそうしている」のか
    // 「単にVitestのバージョン依存の既定値に乗っているだけ」なのか区別できなかった。
    // 明示することで、将来Vitestの既定値が変わっても(または読んだ人が既定値を
    // 誤解しても)この挙動が保たれるようにする
    clearMocks: true,
    // vi.spyOn(window, "confirm")等のspyは、このプロジェクトの全テストが
    // 自分自身で毎回明示的にmockReturnValueを設定し直しており(今回の監査で確認済み、
    // 依存している箇所は無い)実害は無いが、restoreMocksが既定でfalseのままだと
    // 「あるテストがspyOnし忘れた場合に前のテストの戻り値をこっそり引き継いでしまう」
    // という将来の潜在的な分離漏れがある。restoreMocksを有効にすることで、
    // そのような書き忘れが「real window.confirmが呼ばれてエラーになる」という
    // 分かりやすい失敗として即座に露見するようにする
    restoreMocks: true,
  },
});
