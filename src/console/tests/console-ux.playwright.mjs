import { expect, test } from "@playwright/test";
import { Buffer } from "node:buffer";
import { inflateSync } from "node:zlib";

const desktopViewport = { width: 1440, height: 920 };
const mobileViewport = { width: 390, height: 844 };
const pngSignature = [137, 80, 78, 71, 13, 10, 26, 10];

function paethPredictor(left, up, upLeft) {
  const estimate = left + up - upLeft;
  const leftDistance = Math.abs(estimate - left);
  const upDistance = Math.abs(estimate - up);
  const upLeftDistance = Math.abs(estimate - upLeft);

  if (leftDistance <= upDistance && leftDistance <= upLeftDistance) {
    return left;
  }

  if (upDistance <= upLeftDistance) {
    return up;
  }

  return upLeft;
}

function bytesPerPixelForColorType(colorType) {
  if (colorType === 0) {
    return 1;
  }

  if (colorType === 2) {
    return 3;
  }

  if (colorType === 4) {
    return 2;
  }

  if (colorType === 6) {
    return 4;
  }

  expect(colorType, "unsupported PNG color type").toBe(6);
  return 4;
}

function rgbaForPixel(row, column, colorType, bytesPerPixel) {
  const index = column * bytesPerPixel;

  if (colorType === 0) {
    const value = row[index];
    return [value, value, value, 255];
  }

  if (colorType === 2) {
    return [row[index], row[index + 1], row[index + 2], 255];
  }

  if (colorType === 4) {
    const value = row[index];
    return [value, value, value, row[index + 1]];
  }

  return [row[index], row[index + 1], row[index + 2], row[index + 3]];
}

function analyzePngScreenshot(png) {
  expect(Array.from(png.subarray(0, 8)), "PNG signature").toEqual(pngSignature);

  let offset = 8;
  let width = 0;
  let height = 0;
  let bitDepth = 0;
  let colorType = 0;
  const idatChunks = [];

  while (offset < png.length) {
    const length = png.readUInt32BE(offset);
    const chunkTypeOffset = offset + 4;
    const chunkDataOffset = offset + 8;
    const chunkType = png.toString("ascii", chunkTypeOffset, chunkTypeOffset + 4);
    const chunkData = png.subarray(chunkDataOffset, chunkDataOffset + length);

    if (chunkType === "IHDR") {
      width = chunkData.readUInt32BE(0);
      height = chunkData.readUInt32BE(4);
      bitDepth = chunkData[8];
      colorType = chunkData[9];
    }

    if (chunkType === "IDAT") {
      idatChunks.push(chunkData);
    }

    offset = chunkDataOffset + length + 4;
  }

  expect(bitDepth, "PNG bit depth").toBe(8);
  expect(idatChunks.length, "PNG image data chunks").toBeGreaterThan(0);

  const bytesPerPixel = bytesPerPixelForColorType(colorType);
  const stride = width * bytesPerPixel;
  const inflated = inflateSync(Buffer.concat(idatChunks));
  let sourceOffset = 0;
  let previousRow = new Uint8Array(stride);
  const uniqueColors = new Set();
  const sampleEvery = Math.max(1, Math.floor((width * height) / 5000));
  let sampledPixels = 0;
  let visibleSamples = 0;
  let absolutePixel = 0;

  for (let rowIndex = 0; rowIndex < height; rowIndex += 1) {
    const filter = inflated[sourceOffset];
    sourceOffset += 1;

    const row = new Uint8Array(stride);

    for (let byteIndex = 0; byteIndex < stride; byteIndex += 1) {
      const rawValue = inflated[sourceOffset + byteIndex];
      const left = byteIndex >= bytesPerPixel ? row[byteIndex - bytesPerPixel] : 0;
      const up = previousRow[byteIndex] ?? 0;
      const upLeft =
        byteIndex >= bytesPerPixel ? previousRow[byteIndex - bytesPerPixel] : 0;
      const predictor =
        filter === 1
          ? left
          : filter === 2
            ? up
            : filter === 3
              ? Math.floor((left + up) / 2)
              : filter === 4
                ? paethPredictor(left, up, upLeft)
                : 0;

      row[byteIndex] = (rawValue + predictor) & 255;
    }

    for (let column = 0; column < width; column += 1) {
      if (absolutePixel % sampleEvery === 0) {
        const [red, green, blue, alpha] = rgbaForPixel(
          row,
          column,
          colorType,
          bytesPerPixel,
        );

        sampledPixels += 1;

        if (alpha > 0) {
          visibleSamples += 1;
          uniqueColors.add(`${red},${green},${blue},${alpha}`);
        }
      }

      absolutePixel += 1;
    }

    sourceOffset += stride;
    previousRow = row;
  }

  return {
    height,
    sampledPixels,
    uniqueColorCount: uniqueColors.size,
    visibleSamples,
    width,
  };
}

async function expectRenderedScreenshot(locator, label) {
  await locator.scrollIntoViewIfNeeded();

  const screenshot = await locator.screenshot({ animations: "disabled" });
  const analysis = analyzePngScreenshot(screenshot);

  expect(analysis.width, `${label} screenshot width`).toBeGreaterThan(260);
  expect(analysis.height, `${label} screenshot height`).toBeGreaterThan(160);
  expect(analysis.sampledPixels, `${label} sampled pixels`).toBeGreaterThan(250);
  expect(analysis.visibleSamples, `${label} visible pixels`).toBeGreaterThan(250);
  expect(analysis.uniqueColorCount, `${label} color diversity`).toBeGreaterThan(12);
}

async function expectNoViewportOverflow(page) {
  const overflow = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }));

  expect(overflow.scrollWidth).toBeLessThanOrEqual(overflow.clientWidth + 2);
}

async function readBrandVariable(page, variableName) {
  return page.locator(".brand-root").evaluate((node, name) => {
    return globalThis.getComputedStyle(node).getPropertyValue(name).trim();
  }, variableName);
}

test("hub route renders the desktop console shell and workflow atoms", async ({
  page,
}) => {
  await page.setViewportSize(desktopViewport);
  await page.goto("/");

  await expect(page.getByRole("heading", { name: "Change Request Hub" })).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Hub Requests, tasks, and repair work/i }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "Active requests" })).toBeVisible();
  await expect(page.getByText("Jane Rivera")).toBeVisible();
  await expect(page.getByLabel("Live style controls")).toBeVisible();
  expect(await page.locator(".widget").count()).toBeGreaterThanOrEqual(3);

  await expectNoViewportOverflow(page);
  await expectRenderedScreenshot(page.locator(".workflow-page"), "hub desktop");
});

test("basic controls route renders representative atomic field categories", async ({
  page,
}) => {
  await page.setViewportSize(desktopViewport);
  await page.goto("/controls/basic-inputs");

  await expect(
    page.getByRole("heading", { name: "Basic Input Controls" }),
  ).toBeVisible();
  await expect(page.getByText("Primitive text and numeric inputs")).toBeVisible();
  await expect(page.getByText("Date and time bounds")).toBeVisible();
  await expect(page.getByText("Format and pattern enforcement")).toBeVisible();
  await expect(page.getByText("Locked and generated values")).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Text input" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Textarea" })).toBeVisible();
  await expect(page.getByRole("spinbutton", { name: "Number" })).toBeVisible();
  await expect(page.getByRole("textbox", { exact: true, name: "Date" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Time" })).toBeVisible();
  expect(await page.locator("input, textarea, select").count()).toBeGreaterThanOrEqual(
    24,
  );

  await expectNoViewportOverflow(page);
  await expectRenderedScreenshot(
    page.locator(".workflow-page"),
    "basic controls desktop",
  );
});

test("widget route renders reusable data visualization components", async ({
  page,
}) => {
  await page.setViewportSize(desktopViewport);
  await page.goto("/widgets/data-viz");

  await expect(
    page.getByRole("heading", { name: "Data Visualization Widgets" }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { exact: true, name: "Gauge" })).toBeVisible();
  await expect(
    page.getByRole("heading", { exact: true, name: "Line chart" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { exact: true, name: "Heatmap" }),
  ).toBeVisible();
  expect(await page.locator(".widget").count()).toBeGreaterThanOrEqual(24);

  await expectNoViewportOverflow(page);
  await expectRenderedScreenshot(
    page.locator(".workflow-page"),
    "data visualization widgets desktop",
  );
});

test("mobile viewport stacks navigation, content, and style rail without overflow", async ({
  page,
}) => {
  await page.setViewportSize(mobileViewport);
  await page.goto("/controls/choice-selection");

  await expect(
    page.getByRole("heading", { name: "Choice And Selection Controls" }),
  ).toBeVisible();
  await expect(
    page.getByText("Dropdowns, radios, multi-select, and checkbox controls"),
  ).toBeVisible();
  await expect(page.getByLabel("Live style controls")).toBeVisible();

  const shellColumns = await page.locator(".app-shell").evaluate((node) => {
    return globalThis.getComputedStyle(node).gridTemplateColumns.trim().split(/\s+/)
      .length;
  });
  const navColumns = await page.locator(".app-nav").evaluate((node) => {
    return globalThis.getComputedStyle(node).gridTemplateColumns.trim().split(/\s+/)
      .length;
  });

  expect(shellColumns).toBe(1);
  expect(navColumns).toBe(1);

  await expectNoViewportOverflow(page);
  await expectRenderedScreenshot(page.locator(".app-main"), "choice controls mobile");
});

test("style rail updates brand variables used by generated controls", async ({
  page,
}) => {
  await page.setViewportSize(desktopViewport);
  await page.goto("/controls/basic-inputs");

  await expect(page.getByLabel("Live style controls")).toBeVisible();
  await expect.poll(() => readBrandVariable(page, "--surface-base")).toBe("#f5f7f7");
  await expect
    .poll(() => readBrandVariable(page, "--action-primary-background"))
    .toBe("#0f6b5f");

  await page.getByLabel(/Page surface/).fill("#eef6ff");
  await page.getByLabel(/Accent/).fill("#b83280");
  await page.getByLabel(/Density/).selectOption("comfortable");

  await expect.poll(() => readBrandVariable(page, "--surface-base")).toBe("#eef6ff");
  await expect
    .poll(() => readBrandVariable(page, "--action-primary-background"))
    .toBe("#b83280");
  await expect.poll(() => readBrandVariable(page, "--ui-control-height")).toBe("48px");
  await expect
    .poll(() =>
      page
        .locator(".widget")
        .first()
        .evaluate((node) => {
          return globalThis
            .getComputedStyle(node)
            .getPropertyValue("--action-primary-background")
            .trim();
        }),
    )
    .toBe("#b83280");

  await expectRenderedScreenshot(
    page.locator(".workflow-page"),
    "rebranded controls desktop",
  );
});
