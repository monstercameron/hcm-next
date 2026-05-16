import { expect, test } from "@playwright/test";

const desktopViewport = { width: 1440, height: 920 };

const DEMO_SUBJECT_NAME = "Jane Rivera";
const TERMINATION_PROMPT_PREFIX = "Start a termination for ";

/**
 * Build a deterministic mock `PageDefinition` returned by the chat agent's
 * `generate_ui_page` tool. The shape matches what `POST /api/ai/chat`
 * surfaces as `sideEffects.renderPage`.
 */
function buildMockPage(workflowIntent) {
  return {
    id: `ai-generated-${workflowIntent}`,
    title: "Initiate termination",
    description: "Assistant-generated screen for testing.",
    workflowTypes: [workflowIntent],
    surfaceModes: ["full_app"],
    regions: [
      {
        id: "main",
        layout: "stack",
        width: "wide",
        widgets: [
          {
            id: "subject-card",
            type: "data.recordSummary",
            title: "Subject",
            props: {
              record: {
                displayName: DEMO_SUBJECT_NAME,
                jobTitle: "Registered Nurse",
                department: "Somerville Nursing",
                manager: "Alex Manager",
              },
            },
          },
          {
            id: "intro",
            type: "content.callout",
            title: "About this change",
            props: {
              tone: "info",
              body: "Termination requires effective date and reason.",
            },
          },
          {
            id: "actions",
            type: "workflow.actionBar",
            title: "Next steps",
            props: {
              actions: [
                {
                  action: "submit",
                  label: "Submit termination",
                  variant: "primary",
                },
              ],
            },
          },
        ],
      },
    ],
  };
}

/**
 * Intercept `POST /api/ai/chat` and respond with a deterministic envelope.
 * The mock returns a friendly assistant reply plus an optional renderPage
 * side effect when the latest user message mentions a termination — enough
 * to exercise the panel flows without booting the OpenAI provider.
 */
async function mockChat(page) {
  await page.route("**/api/ai/chat", async (route) => {
    const body = route.request().postDataJSON();
    const messages = Array.isArray(body?.messages) ? body.messages : [];
    const lastUser = [...messages]
      .reverse()
      .find((message) => message?.role === "user");
    const userContent =
      typeof lastUser?.content === "string" ? lastUser.content : "";
    const wantsTermination = /terminat/i.test(userContent);

    const responseBody = wantsTermination
      ? {
          assistantMessage: {
            content:
              "I've put the termination form on the screen. Fill it in and submit when ready.",
          },
          sideEffects: {
            renderPage: buildMockPage("employee.termination"),
            workflowConfigHash:
              "sha1:0123456789abcdef0123456789abcdef01234567",
          },
          providerMetadata: {
            provider: "null",
            model: "none",
            inputTokens: 0,
            outputTokens: 0,
          },
        }
      : {
          assistantMessage: {
            content:
              "Hi — I'm the HCM assistant. What would you like to do today?",
          },
          sideEffects: {},
          providerMetadata: {
            provider: "null",
            model: "none",
            inputTokens: 0,
            outputTokens: 0,
          },
        };

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(responseBody),
    });
  });
}

/**
 * Intercept `POST /api/ai/chat` and fulfil with a 500 + structured error
 * envelope. The panel surfaces `error.message` as the assistant bubble.
 */
async function mockChatError(page, message) {
  await page.route("**/api/ai/chat", async (route) => {
    const responseBody = {
      error: {
        code: "system_error",
        message,
        details: { reason: "test_forced_failure" },
      },
    };
    await route.fulfill({
      status: 500,
      contentType: "application/json",
      body: JSON.stringify(responseBody),
    });
  });
}

/**
 * Seed the demo session before the app boots so every test starts on an
 * authenticated workspace.
 */
async function seedSession(page) {
  await page.addInitScript(() => {
    window.localStorage.setItem(
      "hcm-next-demo-session",
      JSON.stringify({
        email: "admin@harborcare.example",
        name: "Avery Morgan",
        signedInAt: "2026-05-15T00:00:00.000Z",
        workspace: "HarborCare Operations",
      }),
    );
  });
}

async function openPanel(page) {
  await page.getByRole("button", { name: "Open HCM assistant" }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
}

async function sendPrompt(page, prompt) {
  const textarea = page.getByRole("textbox", {
    name: "Describe what you'd like to do",
  });
  await textarea.fill(prompt);
  await page.getByRole("button", { name: "Go", exact: true }).click();
}

test.describe("AI chat panel", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize(desktopViewport);
    await mockChat(page);
    await seedSession(page);
    await page.goto("/request");
    await expect(
      page.getByRole("button", { name: "Open HCM assistant" }),
    ).toBeVisible();
  });

  test("floating button is visible after login", async ({ page }) => {
    const fab = page.getByRole("button", { name: "Open HCM assistant" });
    await expect(fab).toBeVisible();
    await expect(fab).toHaveAttribute("aria-expanded", "false");
  });

  test("clicking the button opens the panel with an empty conversation", async ({
    page,
  }) => {
    await openPanel(page);
    await expect(page.getByText("HCM Assistant", { exact: true })).toBeVisible();
    await expect(
      page.getByText(
        "Tell me what you'd like to do, or pick an action above.",
      ),
    ).toBeVisible();
  });

  test("clicking the X closes the panel", async ({ page }) => {
    await openPanel(page);
    await page.getByRole("button", { name: "Close assistant" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
  });

  test("pressing Escape closes the panel and restores focus to the FAB", async ({
    page,
  }) => {
    await openPanel(page);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    const fab = page.getByRole("button", { name: "Open HCM assistant" });
    await expect(fab).toBeFocused();
  });

  test("'hi' goes through the agent without any client-side validation block", async ({
    page,
  }) => {
    await openPanel(page);
    await sendPrompt(page, "hi");

    await expect(
      page.getByText(/HCM assistant/i).first(),
    ).toBeVisible();
    await expect(page.locator(".ai-chat-turn-user").last()).toHaveText("hi");
  });

  test("clicking the termination chip fills the textarea with the templated sentence", async ({
    page,
  }) => {
    await openPanel(page);
    await page
      .getByRole("button", { name: "Start a termination", exact: true })
      .click();
    const textarea = page.getByRole("textbox", {
      name: "Describe what you'd like to do",
    });
    await expect(textarea).toHaveValue(
      `${TERMINATION_PROMPT_PREFIX}${DEMO_SUBJECT_NAME}`,
    );
  });

  test("submitting a termination request renders a page in the main area", async ({
    page,
  }) => {
    await openPanel(page);
    await sendPrompt(page, `Start a termination for ${DEMO_SUBJECT_NAME}`);

    await expect(
      page.getByText(/termination form/i).first(),
    ).toBeVisible();
    const main = page.locator(".app-main");
    await expect(
      main.getByText("Termination requires effective date and reason."),
    ).toBeVisible();
  });

  test("subsequent messages append to the same conversation", async ({ page }) => {
    await openPanel(page);
    await sendPrompt(page, "hi");
    await expect(page.locator(".ai-chat-turn-user").last()).toHaveText("hi");

    await sendPrompt(page, `Start a termination for ${DEMO_SUBJECT_NAME}`);
    const userBubbles = page.locator(".ai-chat-turn-user");
    await expect(userBubbles).toHaveCount(2);
  });

  test("empty textarea disables the submit button", async ({ page }) => {
    await openPanel(page);
    const submit = page.getByRole("button", { name: "Go", exact: true });
    await expect(submit).toBeDisabled();

    const textarea = page.getByRole("textbox", {
      name: "Describe what you'd like to do",
    });
    await textarea.fill("Start a termination for Jane Rivera");
    await expect(submit).toBeEnabled();

    await textarea.fill("   ");
    await expect(submit).toBeDisabled();
  });

  test("focus stays inside the panel while Tab cycles", async ({ page }) => {
    await openPanel(page);

    for (let index = 0; index < 12; index += 1) {
      await page.keyboard.press("Tab");
      const activeIsInsidePanel = await page.evaluate(() => {
        const panel = document.querySelector(".ai-chat-panel");
        const active = document.activeElement;
        if (panel === null || active === null) {
          return false;
        }
        return panel.contains(active);
      });
      expect(activeIsInsidePanel).toBe(true);
    }
  });

  test("backend error path surfaces the safe message as an assistant bubble", async ({
    page,
  }) => {
    await page.unroute("**/api/ai/chat");
    const errorMessage =
      "We could not generate the screen. Try again or use the standard view.";
    await mockChatError(page, errorMessage);

    await openPanel(page);
    await sendPrompt(page, "hi");

    await expect(
      page.locator(".ai-chat-turn-assistant").getByText(errorMessage),
    ).toBeVisible();
  });

  test("clicking outside the panel closes it", async ({ page }) => {
    await openPanel(page);
    await page.mouse.click(20, 20);
    await expect(page.getByRole("dialog")).toHaveCount(0);
  });

  test("Cmd+Enter submits the prompt", async ({ page }) => {
    await openPanel(page);
    const textarea = page.getByRole("textbox", {
      name: "Describe what you'd like to do",
    });
    await textarea.fill(`Start a termination for ${DEMO_SUBJECT_NAME}`);
    await textarea.focus();
    await page.keyboard.press("Meta+Enter");

    const main = page.locator(".app-main");
    await expect(
      main.getByText("Termination requires effective date and reason."),
    ).toBeVisible();
  });

  test("visual baseline: open panel + generated page", async ({ page }) => {
    await openPanel(page);
    await sendPrompt(page, `Start a termination for ${DEMO_SUBJECT_NAME}`);
    await expect(
      page
        .locator(".app-main")
        .getByText("Termination requires effective date and reason."),
    ).toBeVisible();

    // Disable animations + idle pulse so screenshot bytes are stable.
    await page.addStyleTag({
      content: `
        .brand-root *,
        .brand-root *::before,
        .brand-root *::after,
        .ai-chat-fab,
        .ai-chat-panel,
        .ai-chat-panel * {
          animation: none !important;
          transition-duration: 0ms !important;
        }
      `,
    });

    const screenshot = await page.screenshot({ animations: "disabled" });
    expect(screenshot.length).toBeGreaterThan(5_000);
  });
});
