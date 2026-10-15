import { defineConfig } from "@playwright/test";
import { existsSync } from "node:fs";

export default defineConfig({
  testDir: "./e2e",
  testIgnore: /signin\.spec\.ts$/,
  timeout: 60000,
  workers: 1,
  reporter: "list",
  outputDir: "../../.local/playwright-results",
  use: {
    baseURL: "http://127.0.0.1:5173",
    trace: "off",
    screenshot: "off",
    video: "off",
    launchOptions: {
      ...(existsSync("/usr/bin/chromium")
        ? { executablePath: "/usr/bin/chromium" }
        : {}),
    },
  },
  webServer: [
    {
      command: "cd ../.. && node scripts/with-env.mjs ./bin/api",
      url: "http://127.0.0.1:8080/readyz",
      timeout: 30000,
      reuseExistingServer: false,
      stdout: "ignore",
      stderr: "ignore",
    },
    {
      command: "npm run dev",
      url: "http://127.0.0.1:5173",
      timeout: 30000,
      reuseExistingServer: false,
    },
  ],
});
