const assert = require("node:assert/strict");
const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");
const postcss = require("postcss");
const { chromium } = require("playwright");

const demoDirectory = path.resolve(__dirname, "..");
const htmlPath = path.join(demoDirectory, "hcm-brandable-workspace.html");
const cssPath = path.join(__dirname, "workspace.css");
const scriptPath = path.join(__dirname, "workspace.js");
const html = fs.readFileSync(htmlPath, "utf8");
const css = fs.readFileSync(cssPath, "utf8");
const script = fs.readFileSync(scriptPath, "utf8");

const ids = [...html.matchAll(/\bid="([^"]+)"/g)].map((match) => match[1]);
assert.equal(new Set(ids).size, ids.length, "HTML IDs must be unique");
assert(!html.includes("<iframe"), "The product slice must be a native document");
assert.match(html, /id="hcm-latency"/);
assert.match(script, /async function navigate/);
assert.match(script, /function openModal/);
assert.match(script, /Saving decision/);

const sheet = postcss.parse(css);
let hasFocusStyle = false;
let hasReducedMotion = false;
let hasProgressStyle = false;
sheet.walkRules((rule) => {
  if (rule.selector.includes(":focus-visible")) hasFocusStyle = true;
  if (rule.selector.includes(".hcm-route-progress")) hasProgressStyle = true;
});
sheet.walkAtRules("media", (rule) => {
  if (rule.params.includes("prefers-reduced-motion")) hasReducedMotion = true;
});
assert(hasFocusStyle && hasReducedMotion && hasProgressStyle);

const contentTypes = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
};

function startServer() {
  const server = http.createServer((request, response) => {
    const pathname = decodeURIComponent(new URL(request.url, "http://local").pathname);
    const relative =
      pathname === "/" ? "hcm-brandable-workspace.html" : pathname.slice(1);
    const filename = path.resolve(demoDirectory, relative);
    if (!filename.startsWith(demoDirectory + path.sep) || !fs.existsSync(filename)) {
      response.writeHead(404).end("Not found");
      return;
    }
    response.setHeader(
      "Content-Type",
      contentTypes[path.extname(filename)] || "application/octet-stream",
    );
    response.end(fs.readFileSync(filename));
  });
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => resolve(server));
  });
}

async function waitForText(page, selector, expected) {
  await page.waitForFunction(
    ({ selector, expected }) =>
      document.querySelector(selector)?.textContent.includes(expected),
    { selector, expected },
  );
}

(async () => {
  const server = await startServer();
  const { port } = server.address();
  const edge = [
    "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
    "C:/Program Files/Microsoft/Edge/Application/msedge.exe",
  ].find(fs.existsSync);
  let browser;
  let context;
  try {
    browser = await chromium.launch({
      headless: true,
      ...(edge ? { channel: "msedge" } : {}),
    });
    context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    const pageErrors = [];
    page.on("pageerror", (error) => pageErrors.push(error.message));
    await page.route("https://unpkg.com/**", (route) =>
      route.fulfill({ contentType: "text/javascript", body: "" }),
    );
    await page.goto(`http://127.0.0.1:${port}/hcm-brandable-workspace.html`, {
      waitUntil: "domcontentloaded",
    });
    await page.locator("#hcm-customize").click();
    await page.selectOption("#hcm-latency", "350");
    await page.locator("#hcm-customize").click();

    assert.equal(
      await page.locator("#hcm-page-title").textContent(),
      "Good morning, Maya.",
    );
    assert.equal(await page.locator("#hcm-navigation-items [data-page]").count(), 6);

    await page.locator('[data-page="people"]').click();
    await waitForText(page, "#hcm-page-title", "People");
    assert.equal(await page.locator("#hcm-directory [data-person-select]").count(), 6);
    assert.equal(await page.locator("#hcm-directory .hcm-person-context").count(), 1);
    await page.locator('[data-route-action="people-filter"]').click();
    await waitForText(page, "#hcm-directory", "2 people");
    assert.equal(await page.locator("#hcm-directory [data-person-select]").count(), 2);

    await page.locator('[data-page="organization"]').click();
    assert.equal(await page.locator("#hcm-route-progress").isVisible(), true);
    assert.match(
      await page.locator('[data-page="organization"]').textContent(),
      /Loading Organization/,
    );
    await waitForText(page, "#hcm-page-title", "Organization");
    assert.equal(await page.locator('[data-person="jordan"]').count(), 1);

    await page.locator('[data-page="insights"]').click();
    await waitForText(page, "#hcm-page-title", "Insights");
    await page.locator('[data-route-action="export-report"]').click();
    assert.match(
      await page.locator('[data-route-action="export-report"]').textContent(),
      /Preparing/,
    );
    await waitForText(page, "#hcm-toast-region", "Export preview ready");

    await page.locator('[data-page="admin"]').click();
    await waitForText(page, "#hcm-page-title", "Admin");
    assert.equal(await page.locator(".hcm-admin-card").count(), 4);

    await page.locator("#hcm-notifications").click();
    assert.equal(await page.locator("#hcm-notification-panel").isVisible(), true);
    await page.locator('[data-notification-action="mark-read"]').click();
    await waitForText(page, "#hcm-notification-panel", "All caught up");
    assert.equal(
      await page.locator('[data-notification-action="mark-read"]').isDisabled(),
      true,
    );
    await page.locator("#hcm-notifications").click();
    await page.locator("#hcm-notifications").click();
    await waitForText(page, "#hcm-notification-panel", "All caught up");
    await page.locator('[data-notification-work="promotion"]').click();
    await waitForText(page, "#hcm-page-title", "Promotion review");
    await page.locator("[data-preview-decision]").click();
    assert.equal(await page.locator("#hcm-modal-layer").isVisible(), true);
    await page.locator('[data-decision="returned"]').click();
    await waitForText(page, "#hcm-decision-impact", "revision by Alex Morgan");
    assert.equal(
      await page.locator('[data-modal-action="decision-submit"]').textContent(),
      "Return in preview",
    );
    await page.locator('[data-decision="approved"]').click();
    await page.locator('[data-modal-action="decision-submit"]').click();
    assert.match(
      await page.locator('[data-modal-action="decision-submit"]').textContent(),
      /Saving decision/,
    );
    await waitForText(page, "#hcm-detail-content", "Approved locally");
    assert.equal(
      await page.locator('[data-page="work"] .hcm-nav-count').textContent(),
      "3",
    );
    await page.locator("#hcm-back").click();
    await waitForText(page, "#hcm-page-title", "My Work");
    assert.equal(await page.locator("#hcm-work-list [data-work]").count(), 3);
    assert.equal(await page.locator("#hcm-summary-open").textContent(), "3");

    await page.locator('[data-page="home"]').click();
    await waitForText(page, "#hcm-page-title", "Good morning, Maya.");
    await page.locator("#hcm-guide").click();
    await page.keyboard.press("Escape");
    assert.equal(await page.evaluate(() => document.activeElement?.id), "hcm-guide");

    await page.setViewportSize({ width: 360, height: 800 });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
      true,
      "The 360px layout must not overflow horizontally",
    );
    assert.deepEqual(pageErrors, []);

    console.log(
      "PASS: native product slice, responsive layout, delayed routes, pending actions, notifications, governed decision preview, toast receipts, and dialog focus return.",
    );
  } finally {
    await new Promise((resolve) => {
      server.close(resolve);
      server.closeAllConnections?.();
    });
    await context?.close();
    // Installed Edge can retain a background process on Windows. Closing the
    // Playwright transport after its context prevents the smoke test hanging.
    browser?._connection.close(new Error("HCM demo smoke test complete"));
  }
})().then(
  () => process.exit(0),
  (error) => {
    console.error(error);
    process.exit(1);
  },
);
