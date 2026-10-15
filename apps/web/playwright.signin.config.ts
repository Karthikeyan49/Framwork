import { defineConfig } from "@playwright/test";
import { existsSync } from "node:fs";
export default defineConfig({
  testDir: "./e2e", testMatch: "signin.spec.ts", timeout: 60000, workers: 1, reporter: "list", outputDir: "../../.local/playwright-signin-results",
  use: { baseURL: "http://127.0.0.1:5173", trace: "off", screenshot: "off", video: "off", launchOptions: { ...(existsSync("/usr/bin/chromium") ? { executablePath: "/usr/bin/chromium" } : {}) } },
  webServer: [
    { command: "node e2e/oidc-provider.ts", env: { APP_ENV: "test" }, url: "http://127.0.0.1:8090/ready", reuseExistingServer: false, stdout: "ignore", stderr: "ignore" },
    { command: "cd ../.. && node scripts/with-env.mjs ./bin/api", env: { APP_ENV: "test", AUTH_CLOCK_SKEW: "30s", OIDC_CLIENT_ID: "browser-client", OIDC_AUTHORIZATION_URL: "http://127.0.0.1:8090/authorize", OIDC_TOKEN_URL: "http://127.0.0.1:8090/token", OIDC_REDIRECT_URI: "http://127.0.0.1:5173/auth/callback", OIDC_SCOPES: "openid" }, url: "http://127.0.0.1:8080/readyz", timeout: 30000, reuseExistingServer: false, stdout: "ignore", stderr: "ignore" },
    { command: "npm run dev", url: "http://127.0.0.1:5173", reuseExistingServer: false },
  ],
});
