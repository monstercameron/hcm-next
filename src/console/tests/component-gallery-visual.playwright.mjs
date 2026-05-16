import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import path from "node:path";

const desktopViewport = { width: 1440, height: 920 };

const catalogRoutes = [
  "/controls/basic-inputs",
  "/controls/choice-selection",
  "/controls/toggle-slider",
  "/controls/structured-groups",
  "/controls/files-signatures",
  "/controls/hcm-domain",
  "/controls/entity-access",
  "/controls/effective-change",
  "/controls/comp-schedule",
  "/controls/approval-policy-ai",
  "/controls/bulk-repair",
  "/controls/privacy-regional-simulation",
  "/items/content",
  "/items/data-display",
  "/items/workflow-governed",
  "/items/media",
  "/widgets/hcm-insights",
  "/widgets/workflow-ops",
  "/widgets/data-viz",
  "/widgets/time",
  "/widgets/ai-governance",
  "/widgets/documents-evidence",
  "/widgets/collaboration",
  "/widgets/integration",
  "/widgets/ui-blocks",
  "/examples/widgets",
];

const expectedFieldTypes = [
  "text",
  "textarea",
  "number",
  "date",
  "time",
  "select",
  "combobox",
  "multi_select",
  "radio_group",
  "checkbox",
  "toggle",
  "slider",
  "repeater",
  "table",
  "matrix",
  "entity_picker",
  "tree_picker",
  "file_upload",
  "signature",
  "sensitive_reveal",
  "readonly",
];

const expectedWidgetTypes = [
  "layout.section",
  "layout.stack",
  "layout.grid",
  "content.callout",
  "content.text",
  "content.markdown",
  "content.html",
  "content.linkList",
  "content.accordion",
  "data.metricTile",
  "data.progress",
  "data.labelValueList",
  "data.recordSummary",
  "data.checklist",
  "data.queueList",
  "data.table",
  "review.diff",
  "review.timeline",
  "workflow.actionBar",
  "workflow.reasonCapture",
  "viz.chart",
  "viz.graph",
  "ui.board",
  "media.viewer",
  "document.preview",
  "ui.tabs",
  "ui.stepper",
  "ui.modalDrawer",
  "ui.toastCenter",
];

const slug = (value) => value.replace(/[^a-z0-9._-]+/gi, "-");

async function expectNoViewportOverflow(page) {
  const overflow = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }));

  expect(overflow.scrollWidth).toBeLessThanOrEqual(overflow.clientWidth + 2);
}

async function captureAndAudit(locator, screenshotPath, label) {
  await locator.scrollIntoViewIfNeeded();

  const box = await locator.boundingBox();
  expect(box?.width ?? 0, `${label} width`).toBeGreaterThan(80);
  expect(box?.height ?? 0, `${label} height`).toBeGreaterThan(32);

  const quality = await locator.evaluate((root) => {
    const nativeChoiceControls = Array.from(
      root.querySelectorAll('input[type="checkbox"], input[type="radio"]'),
    )
      .filter((node) => {
        const style = window.getComputedStyle(node);

        return style.visibility !== "hidden" && style.display !== "none";
      })
      .map((node) => {
        const rect = node.getBoundingClientRect();

        return {
          height: Math.round(rect.height),
          width: Math.round(rect.width),
        };
      });

    const oversizedNativeControls = nativeChoiceControls.filter(
      (rect) => rect.width > 32 || rect.height > 32,
    );

    const clippingCandidates = Array.from(
      root.querySelectorAll(
        "button, label, .status-badge, .selected-pill, [role='tab']",
      ),
    ).filter((node) => {
      const style = window.getComputedStyle(node);

      return (
        style.visibility !== "hidden" &&
        style.display !== "none" &&
        node.textContent?.trim().length > 0
      );
    });

    const clippedText = clippingCandidates
      .filter(
        (node) =>
          node.scrollWidth > node.clientWidth + 2 ||
          node.scrollHeight > node.clientHeight + 2,
      )
      .map((node) => node.textContent?.trim().slice(0, 80) ?? "");

    return {
      clippedText,
      oversizedNativeControls,
    };
  });

  expect(quality.oversizedNativeControls, `${label} oversized native controls`).toEqual(
    [],
  );
  expect(quality.clippedText, `${label} clipped text`).toEqual([]);

  const screenshot = await locator.screenshot({
    animations: "disabled",
    path: screenshotPath,
  });

  expect(screenshot.length, `${label} screenshot bytes`).toBeGreaterThan(1_000);
}

test("captures every cataloged atomic component type for visual review", async ({
  page,
}, testInfo) => {
  await page.setViewportSize(desktopViewport);

  const screenshotRoot = testInfo.outputPath("component-screenshots");
  await mkdir(screenshotRoot, { recursive: true });

  const seenFields = new Set();
  const seenWidgets = new Set();

  for (const route of catalogRoutes) {
    await page.goto(route);
    await page.addStyleTag({
      content: `
        .brand-root *,
        .brand-root *::before,
        .brand-root *::after {
          animation: none !important;
          transition-duration: 0ms !important;
        }
      `,
    });
    await expectNoViewportOverflow(page);

    const fields = await page.locator(".field-control[data-control-type]").all();

    for (const field of fields) {
      const type = await field.getAttribute("data-control-type");

      if (type === null || seenFields.has(type)) {
        continue;
      }

      seenFields.add(type);
      await captureAndAudit(
        field,
        path.join(screenshotRoot, `field-${slug(type)}.png`),
        `field ${type}`,
      );
    }

    const widgets = await page.locator(".widget[data-widget-canonical-type]").all();

    for (const widget of widgets) {
      const type = await widget.getAttribute("data-widget-canonical-type");

      if (type === null || seenWidgets.has(type)) {
        continue;
      }

      seenWidgets.add(type);
      await captureAndAudit(
        widget,
        path.join(screenshotRoot, `widget-${slug(type)}.png`),
        `widget ${type}`,
      );
    }
  }

  expect(
    expectedFieldTypes.filter((type) => !seenFields.has(type)).sort(),
    "field type screenshot coverage",
  ).toEqual([]);
  expect(
    expectedWidgetTypes.filter((type) => !seenWidgets.has(type)).sort(),
    "widget type screenshot coverage",
  ).toEqual([]);
});
