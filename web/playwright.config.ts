import { defineConfig, devices } from "@playwright/test";

// E2E smoke tests run against the real Conduit binary (Go server + embedded UI).
// By default Playwright launches ./conduitd (built with `make build`) on :8080;
// set E2E_BASE_URL to point at an already-running server instead.
const baseURL = process.env.E2E_BASE_URL || "http://127.0.0.1:8080";
const conduitBin = process.env.CONDUIT_BIN || "../conduitd";

export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  expect: { timeout: 30_000 },
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: [["list"]],
  use: {
    baseURL,
    trace: "retain-on-failure",
    launchOptions: { args: ["--no-sandbox"] },
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  // Only manage the server when E2E_BASE_URL is not provided.
  webServer: process.env.E2E_BASE_URL
    ? undefined
    : {
        command: conduitBin,
        url: "http://127.0.0.1:8080/api/health",
        timeout: 60_000,
        reuseExistingServer: !process.env.CI,
        env: {
          CONDUIT_ADDR: ":8080",
          CONDUIT_ALLOW_PRIVATE_EGRESS: "true",
          CONDUIT_ALLOW_SIGNUP: "true",
        },
      },
});
