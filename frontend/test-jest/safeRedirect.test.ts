import { safeRedirectPath } from "../src/shared/url/safeRedirect";

// src/shared/url/safeRedirect.test.ts (Vitest版)と同じ内容のJest版
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
