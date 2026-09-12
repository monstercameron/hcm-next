// UXAUDIT-001 real-browser evidence against the live, gRPC-backed dev
// server -- not the static SSR/GWC fixtures the other specs in this
// directory replay from tools/uxqual/testdata/rendered. The whole point of
// this todo is what a narrow viewport actually renders and how the overlay
// drawer actually behaves once WASM hydrates it: a pre-baked fixture can
// show the closed, default markup (which the Go tests already pin) but
// cannot open the drawer, trap focus, or measure real layout boxes. This
// spec drives the actual running dev server the way
// promoux001_people_eligibility.spec.mjs does.
//
// The closed-by-default markup, the dialog's role/aria-modal/aria-label
// contract, the trigger's aria-expanded/aria-haspopup/aria-controls wiring,
// the single-row header's CSS grid, and the off-canvas/backdrop CSS are all
// proven structurally in Go: TestTodo_UXAUDIT_001,
// TestTodo_UXAUDIT_001_Browser, TestTodo_UXAUDIT_001_Accessibility,
// TestTodo_UXAUDIT_001_Performance and TestTodo_UXAUDIT_001_Regression in
// internal/humanwork/productui. Tab-wrapping and focus restoration at the
// DOM level are proven in
// TestDrawerFocusTrapWrapsTabAndRestoresFocusOnClose
// (internal/humanwork/productui/drawer_focus_wasm_test.go, a js/wasm-only
// unit test with a synthetic DOM). What none of that can show is the real
// measured layout the 2026-09-12 audit flagged -- competing scroll regions,
// main starting 60%+ down the viewport, a header that wraps to two rows --
// and whether the fix holds in an actual Chromium tab against the actual
// running cell. That is this spec's job.
//
// This spec has NOT been run. It is written to the same conventions as the
// other specs in this directory (test.describe/test, page.getByRole,
// page.evaluate) but targets the live dev server on :8080 rather than a
// file:// fixture, so it needs that server running before it can pass --
// exactly the live verification this todo's brief reserves for the
// operator's own session, not this lane.
import { test, expect } from "@playwright/test";

// Relative on purpose: Playwright resolves it against use.baseURL in
// playwright.config.mjs, which is the module that owns the environment
// override. Reading the environment here would violate the repository's
// process-env-boundary rule.
const homeURL = "/workspace/app/home";

const narrowViewports = [
  { name: "375x812", width: 375, height: 812 },
  { name: "320x720", width: 320, height: 720 },
];

for (const viewport of narrowViewports) {
  test.describe(`UXAUDIT-001 mobile shell at ${viewport.name}`, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });
      await page.goto(homeURL);
    });

    test("the closed drawer contributes no second page-level scroller", async ({
      page,
    }) => {
      const scrollers = await page.evaluate(() => {
        const found = [];
        for (const el of document.querySelectorAll("body *")) {
          const style = window.getComputedStyle(el);
          const scrollableStyle =
            style.overflowY === "auto" || style.overflowY === "scroll";
          const visible =
            el.getClientRects().length > 0 && style.visibility !== "hidden";
          const overflows = el.scrollHeight > el.clientHeight + 1;
          if (scrollableStyle && visible && overflows) {
            found.push({ id: el.id, className: String(el.className) });
          }
        }
        return found;
      });
      expect(
        scrollers,
        "exactly one page-level scroll region: the content's own main-scroll",
      ).toEqual([{ id: "", className: expect.stringContaining("main-scroll") }]);
    });

    test("main content starts within the top 15% of the viewport", async ({ page }) => {
      const main = page.locator("#main-content");
      await expect(main).toBeVisible();
      const box = await main.boundingBox();
      expect(box, "main-content has no layout box").not.toBeNull();
      expect(
        box.y,
        `main starts at y=${box.y}, want within the top 15% of a ${viewport.height}px viewport`,
      ).toBeLessThanOrEqual(viewport.height * 0.15);
    });

    test("the header is a single row", async ({ page }) => {
      const topbar = page.locator(".topbar");
      await expect(topbar).toBeVisible();
      const rowTops = await topbar.evaluate((el) =>
        Array.from(el.children)
          .filter((child) => window.getComputedStyle(child).display !== "none")
          .map((child) => Math.round(child.getBoundingClientRect().top)),
      );
      const distinctRows = new Set(rowTops.map((top) => Math.round(top / 4)));
      expect(
        distinctRows.size,
        `topbar children sit at rows ${JSON.stringify(rowTops)}, want one row`,
      ).toBeLessThanOrEqual(1);
      const headerBox = await topbar.boundingBox();
      expect(
        headerBox.height,
        `header height ${headerBox.height}px, want a single compact row`,
      ).toBeLessThan(90);
    });

    test("no horizontal overflow", async ({ page }) => {
      const overflow = await page.evaluate(
        () =>
          document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      expect(
        overflow,
        "narrow viewport must not scroll horizontally",
      ).toBeLessThanOrEqual(0);
    });

    test("the drawer opens as an accessible dialog, traps focus, and restores it on close", async ({
      page,
    }) => {
      const trigger = page.locator("#nav-drawer-trigger");
      await expect(trigger).toBeVisible();
      await expect(trigger).toHaveAttribute("aria-expanded", "false");

      await trigger.click();
      await expect(trigger).toHaveAttribute("aria-expanded", "true");
      const nav = page.locator("#workspace-navigation");
      await expect(nav).toHaveAttribute("role", "dialog");
      await expect(nav).toHaveAttribute("aria-modal", "true");
      await expect(nav).toHaveAccessibleName();

      // Focus moved into the dialog, not left behind on the trigger.
      const focusedInsideOnOpen = await page.evaluate(() =>
        document
          .getElementById("workspace-navigation")
          ?.contains(document.activeElement),
      );
      expect(
        focusedInsideOnOpen,
        "opening the drawer did not move focus inside it",
      ).toBe(true);

      // Tabbing forward never leaves the dialog while it is open.
      for (let i = 0; i < 40; i++) {
        await page.keyboard.press("Tab");
        const insideDialog = await page.evaluate(() =>
          document
            .getElementById("workspace-navigation")
            ?.contains(document.activeElement),
        );
        expect(
          insideDialog,
          `Tab press ${i + 1} moved focus outside the open drawer`,
        ).toBe(true);
      }

      await page.keyboard.press("Escape");
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
      const restoredToTrigger = await page.evaluate(
        () => document.activeElement?.id === "nav-drawer-trigger",
      );
      expect(
        restoredToTrigger,
        "closing the drawer did not restore focus to its trigger",
      ).toBe(true);
    });
  });
}
