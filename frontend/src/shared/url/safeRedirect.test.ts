import { describe, expect, it } from "vitest";
import { safeRedirectPath } from "./safeRedirect";

// 【3回目のテスト監査で追加】Open Redirect対策(CWE-601)の回帰テスト
// LoginForm.tsxの`?redirect=`クエリパラメータ経由で実際に悪用可能だった
// パターン(userinfo混入によるホスト偽装・protocol-relative URL・絶対URL)を
// 網羅し、いずれもfallbackへ倒れることを保証する
describe("safeRedirectPath", () => {
  it("単一の/で始まる通常の相対パスはそのまま返す", () => {
    expect(safeRedirectPath("/tasks")).toBe("/tasks");
    expect(safeRedirectPath("/labels?foo=bar")).toBe("/labels?foo=bar");
  });

  it("nullish/空文字はfallbackを返す", () => {
    expect(safeRedirectPath(null)).toBe("/tasks");
    expect(safeRedirectPath(undefined)).toBe("/tasks");
    expect(safeRedirectPath("")).toBe("/tasks");
  });

  it("fallbackを指定した場合はそちらを使う", () => {
    expect(safeRedirectPath(null, "/account")).toBe("/account");
  });

  it("絶対URL(http://evil.com)はfallbackへ倒す", () => {
    expect(safeRedirectPath("http://evil.com")).toBe("/tasks");
    expect(safeRedirectPath("https://evil.com/phish")).toBe("/tasks");
  });

  it("userinfo混入によるホスト偽装(@evil.com)はfallbackへ倒す", () => {
    // "/"始まりでない値なので、FrontendBaseURL等と連結された時に
    // "http://localhost:5173@evil.com" のようなホスト偽装が成立してしまう入力
    expect(safeRedirectPath("@evil.com/phish")).toBe("/tasks");
  });

  it("protocol-relative URL(//evil.com)はfallbackへ倒す", () => {
    expect(safeRedirectPath("//evil.com")).toBe("/tasks");
    expect(safeRedirectPath("//evil.com/phish")).toBe("/tasks");
  });

  it("バックスラッシュ始まり(/\\evil.com)はfallbackへ倒す", () => {
    expect(safeRedirectPath("/\\evil.com")).toBe("/tasks");
  });

  it("scheme無しの相対パス(evil.com、先頭/無し)はfallbackへ倒す", () => {
    expect(safeRedirectPath("evil.com")).toBe("/tasks");
  });
});
