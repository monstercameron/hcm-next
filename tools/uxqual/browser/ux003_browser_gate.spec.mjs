// UX-003 real-browser release gates.  These checks intentionally exercise the
// generated SSR/GWC artifacts rather than a mock DOM so keyboard, semantics,
// CSS and authorization regressions are observable in Chromium.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const root = path.dirname(fileURLToPath(import.meta.url));
const rendered = path.resolve(root, "..", "testdata", "rendered");
const docs = ["ssr", "gwc"].map((name) => ({
  name,
  url: pathToFileURL(path.join(rendered, `${name}.html`)).href,
}));

const fields = [
  "proposedJobTitle",
  "proposedCompensation",
  "effectiveDate",
  "businessReason",
];

for (const doc of docs) {
  test.describe(`UX-003 ${doc.name} browser gate`, () => {
    test.beforeEach(async ({ page }) => {
      await page.goto(doc.url);
    });

    test("keyboard focus order is deterministic and has no positive tabindex", async ({
      page,
    }) => {
      const bad = await page
        .locator("[tabindex]")
        .evaluateAll((els) =>
          els
            .filter((el) => Number(el.getAttribute("tabindex")) > 0)
            .map((el) => el.id || el.outerHTML),
        );
      expect(bad, "positive tabindex changes the governed DOM order").toEqual([]);

      await page.locator("body").click({ position: { x: 1, y: 1 } });
      const seen = [];
      for (let i = 0; i < 80 && seen.length < fields.length; i++) {
        await page.keyboard.press("Tab");
        const id = await page.evaluate(() => document.activeElement?.id || "");
        if (fields.includes(id) && !seen.includes(id)) seen.push(id);
      }
      expect(seen).toEqual(fields);
    });

    test("screen-reader landmarks, names, errors and live status are exposed", async ({
      page,
    }) => {
      await expect(page.getByRole("banner")).toBeVisible();
      await expect(page.getByRole("main")).toBeVisible();
      await expect(
        page.getByRole("navigation", { name: "Workspace sections" }),
      ).toBeVisible();
      await expect(page.getByRole("status")).toHaveAttribute("aria-live", "polite");
      for (const id of fields) {
        const control = page.locator(`#${id}`);
        await expect(control).toHaveAccessibleName();
      }
      await expect(page.locator("#reason-reject")).toHaveAccessibleName("Reason");
      for (const id of ["proposedCompensation", "effectiveDate", "businessReason"]) {
        const described = await page.locator(`#${id}`).getAttribute("aria-describedby");
        expect(described, `${id} must retain its validation description`).toBeTruthy();
        await expect(page.locator(`#${described}`)).toBeVisible();
      }
    });

    test("text and action colors meet AA contrast against their computed backgrounds", async ({
      page,
    }) => {
      const failures = await page.evaluate(() => {
        const rgb = (value) => {
          const m = value.match(/rgba?\\(([^)]+)\\)/);
          if (!m) return null;
          const p = m[1].split(",").map((x) => Number.parseFloat(x.trim()));
          return p.length >= 3 ? p.slice(0, 3).map((n) => n / 255) : null;
        };
        const lum = (c) =>
          c.map((x) => (x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4));
        const ratio = (a, b) => {
          const la = lum(a),
            lb = lum(b);
          const x = 0.2126 * la[0] + 0.7152 * la[1] + 0.0722 * la[2];
          const y = 0.2126 * lb[0] + 0.7152 * lb[1] + 0.0722 * lb[2];
          return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
        };
        return [...document.querySelectorAll("button, .status-banner, .error")].flatMap(
          (el) => {
            const s = window.getComputedStyle(el),
              fg = rgb(s.color),
              bg = rgb(s.backgroundColor);
            return fg &&
              bg &&
              bg.every((x) => x === 0) &&
              s.backgroundColor === "rgba(0, 0, 0, 0)"
              ? []
              : fg && bg && ratio(fg, bg) < 4.5
                ? [{ text: el.textContent.trim(), ratio: ratio(fg, bg) }]
                : [];
          },
        );
      });
      expect(failures).toEqual([]);
    });

    test("200% zoom and 400% reflow remain usable without horizontal overflow", async ({
      page,
    }) => {
      for (const zoom of [2, 4]) {
        await page.setViewportSize({ width: 1280, height: 900 });
        await page.evaluate((z) => {
          document.documentElement.style.zoom = String(z);
        }, zoom);
        const metrics = await page.evaluate(() => ({
          scrollWidth: document.documentElement.scrollWidth,
          clientWidth: document.documentElement.clientWidth,
        }));
        expect(
          metrics.scrollWidth,
          `${doc.name} overflows at ${zoom * 100}%`,
        ).toBeLessThanOrEqual(metrics.clientWidth + 1);
        await page.evaluate(() => {
          document.documentElement.style.zoom = "1";
        });
      }
      await page.setViewportSize({ width: 320, height: 800 });
      const narrow = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      }));
      expect(narrow.scrollWidth).toBeLessThanOrEqual(narrow.clientWidth + 1);
    });

    test("reduced-motion preference disables motion and authorization projection is safe", async ({
      page,
    }) => {
      await page.emulateMedia({ reducedMotion: "reduce" });
      const motion = await page.locator("body").evaluate((el) => {
        const s = window.getComputedStyle(el);
        return { animation: s.animationName, transition: s.transitionDuration };
      });
      expect(motion.animation).toBe("none");
      expect(motion.transition).toBe("0s");
      await expect(
        page.locator('input[name="transition"][value="approve"]'),
      ).toHaveCount(1);
      await expect(
        page.locator('input[name="transition"][value="reject"]'),
      ).toHaveCount(1);
      await expect(
        page.locator('input[name="transition"][value="request_more_information"]'),
      ).toHaveCount(1);
      await expect(
        page.getByText(/force_execute|Force execute|555-11-2222|nationalId/i),
      ).toHaveCount(0);
    });
  });
}
