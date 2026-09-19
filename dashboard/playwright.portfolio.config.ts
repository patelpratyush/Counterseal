import { defineConfig } from "@playwright/test";
import base from "./playwright.config";

// Record the real-API browser walkthrough with enough time to read each screen.
export default defineConfig(base, {
  timeout: 180_000,
  outputDir: "test-results/portfolio",
  use: {
    ...base.use,
    video: { mode: "on", size: { width: 1440, height: 1000 } },
    launchOptions: { slowMo: 500 },
  },
});
