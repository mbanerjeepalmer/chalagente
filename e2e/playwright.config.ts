import { defineConfig, devices } from "@playwright/test";

const PORT = 18080;
const BASE_URL = `http://127.0.0.1:${PORT}`;
const USER = "admin";
const PASS = "e2epass";

export default defineConfig({
  testDir: ".",
  fullyParallel: false,
  reporter: [["list"]],
  use: {
    baseURL: BASE_URL,
    httpCredentials: { username: USER, password: PASS },
  },
  projects: [{ name: "chromium", use: devices["Desktop Chrome"] }],
  webServer: {
    command: `go run ..`,
    cwd: ".",
    url: `${BASE_URL}/healthz`,
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    ignoreHTTPSErrors: true,
    env: {
      HTTP_ONLY: "1",
      HTTP_ADDR: `:${PORT}`,
      BASIC_AUTH_USER: USER,
      BASIC_AUTH_PASS: PASS,
      // Toggle agent availability per-test via separate webServer? Playwright doesn't allow.
      // Default to enabled — tests for the "unavailable" path use a separate page-level check
      // by reading the rendered HTML and asserting the disabled attribute via DOM.
      AWS_BEARER_TOKEN_BEDROCK: "test-token",
      BEDROCK_ENDPOINT: "http://127.0.0.1:1", // never reached in these tests
    },
  },
});
