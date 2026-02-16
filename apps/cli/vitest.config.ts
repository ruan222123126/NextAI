import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["test/**/*.test.ts"],
  },
  coverage: {
    provider: "v8",
    reporter: ["text-summary"],
    thresholds: {
      statements: 55,
      branches: 40,
      functions: 50,
      lines: 55,
    },
  },
});
