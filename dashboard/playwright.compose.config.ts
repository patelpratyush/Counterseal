import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  testMatch: "compose.spec.ts",
  timeout: 60_000,
  workers: 1,
  retries: 0,
  reporter: "list",
  use: {
    baseURL: process.env.HG_COMPOSE_URL || "http://localhost:3137",
    browserName: "chromium",
  },
});
