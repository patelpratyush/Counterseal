import { defineConfig } from "@playwright/test";

const port = Number(process.env.HG_E2E_PORT || 4173);
if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("Invalid HG_E2E_PORT");
for (const key of ["HANDOFFGUARD_SERVER_URL", "HANDOFFGUARD_API_TOKEN", "HANDOFFGUARD_OPERATOR_PASSWORD", "HANDOFFGUARD_VIEWER_PASSWORD"]) {
  if (!process.env[key]) throw new Error(`Missing ${key}; use scripts/smoke-dashboard.sh`);
}
const url = `http://localhost:${port}`;

export default defineConfig({
  testDir: "./tests",
  testMatch: "browser.spec.ts",
  timeout: 90_000,
  workers: 1,
  retries: 0,
  reporter: "list",
  use: { baseURL: url, browserName: "chromium", viewport: { width: 1440, height: 1000 } },
  webServer: {
    command: `npm run start -- --hostname 127.0.0.1 --port ${port}`,
    url: `${url}/login`,
    reuseExistingServer: false,
    timeout: 60_000,
    gracefulShutdown: { signal: "SIGTERM", timeout: 10_000 },
    env: { NEXT_TELEMETRY_DISABLED: "1", HANDOFFGUARD_LOCAL_PREVIEW: "0",
      HANDOFFGUARD_API_TOKEN: "", HANDOFFGUARD_DATABASE_URL: "",
      HANDOFFGUARD_OPERATOR_PASSWORD: "", HANDOFFGUARD_VIEWER_PASSWORD: "" },
  },
});
