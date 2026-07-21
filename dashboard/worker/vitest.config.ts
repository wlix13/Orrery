import { defineConfig } from "vitest/config";

// Own config so vitest does not inherit the SPA's vite setup from the parent directory.
export default defineConfig({
  test: { include: ["src/**/*.test.ts"] },
});
