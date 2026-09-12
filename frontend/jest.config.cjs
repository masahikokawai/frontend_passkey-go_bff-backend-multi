// Jestの設定
// package.jsonの "type": "module" とは独立させるため .cjs 拡張子にしている
//
// Vitest(vitest.config.ts)との対比:
// - Vitestはvite.config.tsの延長でESM/TSをネイティブに理解するのに対し、
//   JestはデフォルトでCommonJS前提のため、ts-jestでTS→CJSへトランスパイルする
//   専用のtsconfig(tsconfig.jest.json、module: "commonjs")を用意している
// - テスト対象はsrc/配下だが、テストコード自体はtest-jest/配下に分離し、
//   Vitest(vitest.config.tsでtest-jest/を除外)と二重実行されないようにしている
module.exports = {
  preset: "ts-jest",
  testEnvironment: "jsdom",
  rootDir: ".",
  testMatch: ["<rootDir>/test-jest/**/*.test.{ts,tsx,jsx}"],
  moduleNameMapper: {
    "^@/(.*)$": "<rootDir>/src/$1",
  },
  transform: {
    "^.+\\.[tj]sx?$": ["ts-jest", { tsconfig: "tsconfig.jest.json" }],
  },
  setupFiles: ["<rootDir>/test-jest/polyfills.ts"],
  setupFilesAfterEnv: ["<rootDir>/test-jest/setup.ts"],
};
