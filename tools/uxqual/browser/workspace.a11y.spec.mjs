// Real-Chromium (via Playwright, already a package.json devDependency)
// supplementary evidence for UX-QUAL-001's qualification fixture. The
// authoritative, CI-gating checks are the Go tests in tools/uxqual/qual
// (structural HTML parsing with golang.org/x/net/html); this spec adds a
// genuine browser pass on top -- real keyboard Tab traversal, Chromium's
// computed accessibility tree, and an actual 320px viewport reflow check --
// against the exact same rendered documents (tools/uxqual/testdata/rendered,
// written by `go run ./tools/uxqual/cmd/genfixtures`).
//
// It does not replace the manual NVDA (Windows) / VoiceOver (macOS) pass
// the go-only technology constitution's fixture names explicitly; that
// remains PENDING in definitions/ux/workspace-renderer-decision.yaml.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const renderedDir = path.resolve(here, "..", "testdata", "rendered");

const maskedNeedles = ["nationalId", "555-11-2222", "force_execute", "Force execute"];

// Field order the fixture (tools/uxqual/testdata/fixture.go) authorizes,
// excluding the read-only currentJobTitle field (rendered as static text,
// not a control) -- this is the DOM-order tab sequence both renderers must
// produce.
const expectedFieldOrder = [
  "proposedJobTitle",
  "proposedCompensation",
  "effectiveDate",
  "businessReason",
];

const documents = [
  { name: "ssr", file: "ssr.html" },
  { name: "gwc", file: "gwc.html" },
];

for (const doc of documents) {
  test.describe(`${doc.name} renderer`, () => {
    const url = pathToFileURL(path.join(renderedDir, doc.file)).href;

    test("never renders a masked field or action", async ({ page }) => {
      await page.goto(url);
      const content = await page.content();
      for (const needle of maskedNeedles) {
        expect(
          content,
          `masked value ${JSON.stringify(needle)} leaked into ${doc.name}`,
        ).not.toContain(needle);
      }
    });

    test("landmarks and a simulation live region are present", async ({ page }) => {
      await page.goto(url);
      await expect(page.getByRole("banner")).toBeVisible();
      await expect(page.getByRole("main")).toBeVisible();
      await expect(page.getByRole("navigation")).toBeVisible();
      await expect(page.getByRole("status")).toBeVisible();
    });

    test("every visible field has an accessible name from its label", async ({
      page,
    }) => {
      await page.goto(url);
      for (const id of expectedFieldOrder) {
        const control = page.locator(`#${id}`);
        await expect(control).toBeVisible();
        const name = await control.evaluate((el) => {
          const label = document.querySelector(`label[for="${el.id}"]`);
          return label ? label.textContent : null;
        });
        expect(name, `#${id} has no associated <label for>`).toBeTruthy();
      }
    });

    test("keyboard-only Tab order follows DOM order with no trap", async ({ page }) => {
      await page.goto(url);
      await page.locator("body").click({ position: { x: 1, y: 1 } });
      await page.keyboard.press("Tab"); // skip link, first stop

      const seen = [];
      for (let i = 0; i < 40; i++) {
        const id = await page.evaluate(
          () => document.activeElement && document.activeElement.id,
        );
        if (id && expectedFieldOrder.includes(id) && !seen.includes(id)) {
          seen.push(id);
        }
        if (seen.length === expectedFieldOrder.length) break;
        await page.keyboard.press("Tab");
      }
      expect(
        seen,
        `Tab traversal never reached all fixture fields in ${doc.name}`,
      ).toEqual(expectedFieldOrder);
    });

    test("reflows at a 320px viewport with no horizontal overflow", async ({
      page,
    }) => {
      await page.setViewportSize({ width: 320, height: 800 });
      await page.goto(url);
      const overflow = await page.evaluate(() => {
        const doc = document.documentElement;
        return { scrollWidth: doc.scrollWidth, clientWidth: doc.clientWidth };
      });
      expect(
        overflow.scrollWidth,
        `document is wider than its 320px viewport in ${doc.name}`,
      ).toBeLessThanOrEqual(overflow.clientWidth + 1);
    });
  });
}
