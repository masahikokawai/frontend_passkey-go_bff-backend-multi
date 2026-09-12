// Jestのグローバルセットアップ(setupFilesAfterEnv)
//
// Vitest側(src/test/setup.ts)との対比: Vitestは vitest.config.ts で globals:false に
// しているため afterEach(cleanup) を明示的に書く必要があったが、Jestは
// describe/it/expect/afterEach が最初からグローバルに存在する前提のツールであり、
// @testing-library/reactの自動クリーンアップ(内部でグローバルのafterEachを検出して
// 登録する仕組み)がそのまま働くため、ここでcleanup()を呼ぶコードを書く必要がない
//
// TextEncoder/TextDecoderのポリフィルは./polyfills.ts(setupFiles、こちらより先に
// 実行される)側で完了済みの前提(このファイルでundiciをimportする時点で必要になるため)
import { fetch, Headers, Request, Response, getGlobalDispatcher } from "undici";
import "@testing-library/jest-dom";

// jest-environment-jsdomはfetch/Response/Headersを標準で持たない(Node自体には
// あるが、jsdom用にJestが用意する専用のglobalオブジェクトには引き継がれない)
// Vitestのjsdom環境では素通しで使えていたため、この差はJest固有のギャップ
// Node.js 18以降が内部で使っているのと同じ実装(undici)から補う
if (typeof globalThis.fetch === "undefined") {
  globalThis.fetch = fetch as unknown as typeof globalThis.fetch;
  globalThis.Headers = Headers as unknown as typeof globalThis.Headers;
  globalThis.Request = Request as unknown as typeof globalThis.Request;
  globalThis.Response = Response as unknown as typeof globalThis.Response;
}

// undiciはimportした時点でグローバルなdispatcher(keep-alive用のコネクション
// プール)を持ってしまい、テストがfetchを一度も実際には呼ばなくても
// Jestのワーカープロセスがハンドルを残したまま終了できなくなる傾向がある
// ("A worker process has failed to exit gracefully"という警告)
// 明示的にcloseしても解消しきらない場合があるが、テスト結果自体(pass/fail)には
// 影響しない既知の警告として許容している
afterAll(async () => {
  await getGlobalDispatcher().close();
});
