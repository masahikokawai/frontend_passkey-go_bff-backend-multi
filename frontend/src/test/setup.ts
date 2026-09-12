// Vitestのグローバルセットアップ
import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// vitest.config.tsで globals: false にしているため、
// @testing-library/reactの自動クリーンアップ(afterEachへの暗黙登録)が効かない
// そのためここで明示的にafterEachごとにcleanup()する
// (呼ばないと、前のテストでrenderしたDOMが残り「複数要素が見つかった」エラーになる)
afterEach(() => {
  cleanup();
});
