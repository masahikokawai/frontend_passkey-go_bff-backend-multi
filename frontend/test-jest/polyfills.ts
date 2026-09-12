// jest.config.cjsの`setupFiles`(setupFilesAfterEnvより先に走る)専用ファイル
//
// TextEncoder/TextDecoderのポリフィルは、setup.ts側でundiciをimportするより
// 前に完了していなければならない(undici自身のモジュール読み込み時点でTextDecoderをグローバルから参照するため)
// 同じファイル内でimport文の後に代入を書いても、ESモジュールのimportは常にファイル本体より先に評価されるため間に合わない
// そのためsetupFiles/setupFilesAfterEnvという2段階にファイル自体を分離し、確実にこちらを先に完了させている
import { TextEncoder, TextDecoder } from "node:util";
import { ReadableStream, WritableStream, TransformStream } from "node:stream/web";
import { MessageChannel, MessagePort } from "node:worker_threads";

if (typeof globalThis.TextEncoder === "undefined") {
  globalThis.TextEncoder = TextEncoder;
  globalThis.TextDecoder = TextDecoder as typeof globalThis.TextDecoder;
}

// undici(setup.tsでfetch/Response/Headersの供給元として使う)自身が、
// jsdomには存在しないWeb Streams API(ReadableStream等)やMessageChannelを
// 要求するため、Node組み込みの実装からここでも補っておく
if (typeof globalThis.ReadableStream === "undefined") {
  globalThis.ReadableStream = ReadableStream as unknown as typeof globalThis.ReadableStream;
  globalThis.WritableStream = WritableStream as unknown as typeof globalThis.WritableStream;
  globalThis.TransformStream = TransformStream as unknown as typeof globalThis.TransformStream;
}
if (typeof globalThis.MessageChannel === "undefined") {
  globalThis.MessageChannel = MessageChannel as unknown as typeof globalThis.MessageChannel;
  globalThis.MessagePort = MessagePort as unknown as typeof globalThis.MessagePort;
}
