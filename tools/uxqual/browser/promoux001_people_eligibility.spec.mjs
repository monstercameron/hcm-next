// PROMOUX-001 real-browser evidence against the live, gRPC-backed
// server -- not the static SSR/GWC fixtures the other specs in this
// directory replay from tools/uxqual/testdata/rendered. This feature's
// whole point is a server-composed, per-worker availability verdict
// (eligible / ineligible / active-conflict / withheld); a pre-baked
// fixture document cannot exercise that, so this spec drives the actual
// running dev server the way tools/uxqual/journeyclient and
// tools/uxqual/productclient serve it.
//
// The exact-once launch and reason-text vocabulary are proven structurally
// in Go (TestTodo_PROMOUX_001, TestTodo_PROMOUX_001_Security,
// TestTodo_PROMOUX_001_Regression in internal/humanwork/productui, and
// TestTodo_PROMOUX_001_Integration in internal/intent/app against a real
// PostgreSQL server). This spec (TestTodo_PROMOUX_001_Browser) is the
// browser-side complement: does the live People directory actually render
// the eligible-only filter and a non-bare reason for a real worker, in a
// real Chromium tab, against the real running cell.
//
// This spec has NOT been run. It is written to the same conventions as the
// other specs in this directory (test.describe/test, page.getByRole,
// page.evaluate) but targets the live dev server on :8080 rather than a
// file:// fixture, so it needs that server running and seeded with the
// demoworkforce corpus before it can pass -- exactly the live verification
// this todo's brief reserves for a human running session, not this lane.
import { test, expect } from "@playwright/test";

// Relative on purpose: Playwright resolves it against use.baseURL in
// playwright.config.mjs, which is the module that owns the environment
// override. Reading the environment here would violate the repository's
// process-env-boundary rule.
const peopleURL = "/workspace/app/people";

test.describe("PROMOUX-001 people directory eligibility", () => {
  test("the eligible-only filter is present and narrows the directory", async ({
    page,
  }) => {
    await page.goto(peopleURL);
    const eligibleFilter = page.locator("#people-eligible-filter");
    await expect(eligibleFilter).toBeAttached();

    const rowCountBefore = await page.locator(".people-row").count();
    await eligibleFilter.check();
    await page.waitForURL(/[?&]eligible=1(&|$)/);
    const rowCountAfter = await page.locator(".people-row").count();
    expect(
      rowCountAfter,
      "eligible-only filter did not narrow the directory",
    ).toBeLessThanOrEqual(rowCountBefore);

    // Every remaining row must actually offer the Promote workflow -- the
    // filter is worthless if it merely relabels rows without excluding
    // ineligible/conflicted/withheld ones.
    const workflowTriggers = page.locator(".people-row .people-row-action");
    const remaining = await workflowTriggers.count();
    expect(
      remaining,
      "eligible-only filter left a row with no launchable workflow",
    ).toBe(rowCountAfter);
  });

  test("a suppressed promotion action always shows a real, non-bare reason", async ({
    page,
  }) => {
    await page.goto(peopleURL);
    // Rows with no launchable workflow render a plain reason span instead
    // of the workflow-menu trigger button (people_components.go's
    // workflowMenu fallback).
    const bareFallback = page.getByText("No available workflows", { exact: true });
    await expect(
      bareFallback,
      "a row still renders the old bare, unexplained fallback",
    ).toHaveCount(0);
  });

  test("landmarks resolve on the live People page", async ({ page }) => {
    await page.goto(peopleURL);
    await expect(page.getByRole("banner")).toBeVisible();
    await expect(page.getByRole("main")).toBeVisible();
    await expect(page.getByRole("navigation").first()).toBeVisible();
  });
});
