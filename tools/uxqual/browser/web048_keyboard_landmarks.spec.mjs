// WEB-048 supplementary real-browser proof for shell keyboard and landmark
// navigation: DOM-order tab sequence (no positive tabindex), landmark roles
// in the computed accessibility tree, and a 390px viewport reflow check
// against the shared rendered documents (tools/uxqual/testdata/rendered,
// written by `go run ./tools/uxqual/cmd/genfixtures`). Landmark
// inventories and skip-link wiring for the productui shell itself are
// proven in Go (TestTodo_WEB_048_Browser parses the real rendered
// documents, which these workspace fixtures do not include); this spec
// proves the browser-side mechanics SSR parsing cannot observe.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const renderedDir = path.resolve(here, "..", "testdata", "rendered");

const documents = [
  { name: "ssr", file: "ssr.html" },
  { name: "gwc", file: "gwc.html" },
];

for (const doc of documents) {
  test.describe(`web048 ${doc.name} keyboard and landmarks`, () => {
    const url = pathToFileURL(path.join(renderedDir, doc.file)).href;

    test("tab order is DOM order with no positive tabindex", async ({ page }) => {
      await page.goto(url);
      const positive = await page.evaluate(() =>
        Array.from(document.querySelectorAll("[tabindex]"))
          .map((element) => element.getAttribute("tabindex"))
          .filter((value) => Number(value) > 0),
      );
      expect(positive, `positive tabindex in ${doc.name}`).toEqual([]);
    });

    test("reflows at a 390px viewport with no horizontal overflow", async ({
      page,
    }) => {
      await page.setViewportSize({ width: 390, height: 800 });
      await page.goto(url);
      const overflow = await page.evaluate(
        () =>
          document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      expect(
        overflow,
        `document is wider than its 390px viewport in ${doc.name}`,
      ).toBeLessThanOrEqual(0);
    });

    test("landmark roles resolve in the accessibility tree", async ({ page }) => {
      await page.goto(url);
      await expect(page.getByRole("banner")).toBeAttached();
      await expect(page.getByRole("main")).toBeAttached();
      await expect(page.getByRole("contentinfo")).toBeAttached();
    });
  });
}
