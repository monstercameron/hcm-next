// Standalone Playwright config for UX-QUAL-001's supplementary real-browser
// pass. It is scoped entirely to tools/uxqual/browser (this lane may not
// edit the repository's existing src/console/playwright.config.mjs or
// package.json), and needs no webServer: the specs load the static
// documents tools/uxqual/cmd/genfixtures writes to
// tools/uxqual/testdata/rendered/*.html directly via file:// URLs.
//
// Generate the fixtures, then run:
//   go run ./tools/uxqual/cmd/genfixtures
//   npx playwright test --config=tools/uxqual/browser/playwright.config.mjs
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./",
  testMatch: "*.spec.mjs",
  fullyParallel: true,
  // This machine routinely runs many concurrent Claude/Codex sessions each
  // driving their own Chrome/Chromium instances, which starves a freshly
  // launched headless-shell process enough to blow the default 30s
  // navigation/setup timeout under load (observed: "Test timeout of 30000ms
  // exceeded while setting up 'page'" with no assertion ever reached). One
  // retry absorbs that contention without weakening any assertion -- a
  // genuine accessibility failure still fails every attempt.
  retries: 1,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  reporter: "list",
  outputDir: "../../../test-results/playwright-uxqual",
  use: {
    colorScheme: "light",
    reducedMotion: "reduce",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
