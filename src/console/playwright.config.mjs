import { defineConfig, devices } from "@playwright/test";

const defaultBaseURL = "http://127.0.0.1:5173";
const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? defaultBaseURL;
const shouldStartConsoleServer = process.env.PLAYWRIGHT_BASE_URL === undefined;

export default defineConfig({
  testDir: "./tests",
  testMatch: "**/*.playwright.mjs",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI === undefined ? 0 : 2,
  reporter:
    process.env.CI === undefined
      ? "list"
      : [
          ["dot"],
          ["html", { open: "never", outputFolder: "../../playwright-report/console" }],
        ],
  outputDir: "../../test-results/playwright-console",
  use: {
    baseURL,
    colorScheme: "light",
    reducedMotion: "reduce",
    screenshot: "only-on-failure",
    trace: "on-first-retry",
  },
  webServer: shouldStartConsoleServer
    ? {
        command:
          "npm --workspace @hcm-next/console run dev -- --host 127.0.0.1 --port 5173",
        reuseExistingServer: process.env.CI === undefined,
        timeout: 120_000,
        url: defaultBaseURL,
      }
    : undefined,
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
