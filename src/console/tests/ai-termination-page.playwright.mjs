// E2E coverage for the AI-generated termination page. This spec uses the
// EXACT shape OpenAI emits in production (fields keyed by `name`, recordId
// bound to `$selectedSubject`, etc.) so regressions in the renderer surface
// here rather than in production. See `ai-chat-panel.playwright.mjs` for the
// chat-panel-only coverage.
import { expect, test } from "@playwright/test";

const desktopViewport = { width: 1440, height: 960 };

function buildLiveAiPageDefinition() {
  return {
    id: "ai-generated-employee.termination",
    title: "Initiate employee termination",
    description: "Start a termination request and submit for HR director approval.",
    workflowTypes: ["employee.termination"],
    surfaceModes: ["full_app"],
    regions: [
      {
        id: "main",
        layout: "stack",
        width: "wide",
        widgets: [
          {
            id: "picker_employee",
            type: "form.subjectPicker",
            title: "Employee",
            props: {
              label: "Select employee to apply this workflow to",
              required: true,
              options: [
                {
                  id: "emp_910",
                  displayName: "Olivia Bennett",
                  jobTitle: "Executive Director",
                  department: "Executive",
                },
                {
                  id: "emp_930",
                  displayName: "Priya Nair",
                  jobTitle: "Director, Clinic Operations",
                  department: "Clinic Operations",
                  manager: "Olivia Bennett",
                },
                {
                  id: "emp_940",
                  displayName: "Grace Kim",
                  jobTitle: "Director, People Operations",
                  department: "People",
                  manager: "Olivia Bennett",
                },
              ],
            },
          },
          {
            id: "summary_employee",
            type: "data.recordSummary",
            title: "Employee details",
            description: "Current profile for the selected employee.",
            props: { recordId: "$selectedSubject" },
          },
          {
            id: "form_termination_details",
            type: "form.dynamicFieldGroup",
            title: "Termination details",
            description:
              "Provide termination type, effective date, and a business reason.",
            props: {
              fields: [
                {
                  // GPT-5-mini emits `name`, not `id`. The renderer must accept it.
                  name: "terminationType",
                  label: "Termination type",
                  type: "select",
                  required: true,
                  options: [
                    { label: "Voluntary", value: "voluntary" },
                    { label: "Involuntary", value: "involuntary" },
                    { label: "Retirement", value: "retirement" },
                    { label: "Mutual agreement", value: "mutual_agreement" },
                  ],
                },
                {
                  name: "effectiveAt",
                  label: "Effective date",
                  type: "date",
                  required: true,
                },
                {
                  name: "businessReason",
                  label: "Business reason",
                  type: "textarea",
                  required: true,
                  helpText: "10-500 characters.",
                },
              ],
            },
          },
          {
            id: "actions_submit",
            type: "workflow.actionBar",
            title: "Submit termination",
            props: {
              actions: [
                {
                  transition: "submit_input",
                  label: "Submit for approval",
                  variant: "primary",
                },
                { transition: "cancel", label: "Cancel", variant: "secondary" },
              ],
            },
          },
        ],
      },
    ],
  };
}

async function seedSession(page) {
  await page.addInitScript(() => {
    window.localStorage.setItem(
      "hcm-next-demo-session",
      JSON.stringify({
        email: "admin@harborcare.example",
        name: "Avery Morgan",
        signedInAt: new Date().toISOString(),
        workspace: "HarborCare Operations",
      }),
    );
    window.localStorage.removeItem("hcm-next:ai-ui-cache:v1");
  });
}

async function mockChatReturningTerminationPage(page) {
  await page.route("**/api/ai/chat", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        assistantMessage: { content: "Termination form is open." },
        sideEffects: {
          renderPage: buildLiveAiPageDefinition(),
          workflowConfigHash: "sha1:eb50f1f5e8c413cc82da60d258f5ae2fa232f94a",
        },
        providerMetadata: {
          provider: "test",
          model: "test",
          inputTokens: 0,
          outputTokens: 0,
        },
      }),
    });
  });
}

test.describe("AI-generated termination page", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize(desktopViewport);
    await mockChatReturningTerminationPage(page);
    await seedSession(page);
    await page.goto("/workspace");
  });

  test("renders real form controls for each AI-declared field (not '0 controls')", async ({
    page,
  }) => {
    await page.getByRole("button", { name: "Open HCM assistant" }).click();
    await expect(page.getByRole("dialog")).toBeVisible();

    const textarea = page.getByRole("textbox", {
      name: "Describe what you'd like to do",
    });
    await textarea.fill("start a termination");
    await page.getByRole("button", { name: "Go", exact: true }).click();

    // Wait for the assistant turn AND the rendered page.
    await expect(page.getByRole("button", { name: "Close assistant" })).toBeVisible();
    await page.keyboard.press("Escape");

    // The form should have THREE real controls, one per AI-declared field.
    // Use role-scoped queries so we hit the inputs, not their constraint-tag
    // aria-label siblings ("Business reason constraints" etc.).
    const terminationTypeSelect = page.getByRole("combobox", {
      name: "Termination type",
    });
    const effectiveDateInput = page.locator("input#effectiveAt");
    const businessReasonTextarea = page.getByRole("textbox", {
      name: "Business reason",
    });

    await expect(terminationTypeSelect).toBeVisible();
    await expect(effectiveDateInput).toBeVisible();
    await expect(businessReasonTextarea).toBeVisible();

    // The "Workflow data / N controls" summary must reflect the real count.
    const summary = page.locator(".form-preview span", { hasText: /controls?$/ });
    await expect(summary).toHaveText(/^3 controls$/);
  });

  test("picking an employee populates the record summary with their details", async ({
    page,
  }) => {
    await page.getByRole("button", { name: "Open HCM assistant" }).click();
    await page
      .getByRole("textbox", { name: "Describe what you'd like to do" })
      .fill("start a termination");
    await page.getByRole("button", { name: "Go", exact: true }).click();
    await expect(page.getByRole("button", { name: "Close assistant" })).toBeVisible();
    await page.keyboard.press("Escape");

    // Pick Priya Nair.
    const picker = page.getByLabel(/select employee/i);
    await picker.selectOption("emp_930");

    // Employee details widget should reflect Priya's profile.
    const recordSummary = page.locator(".employee-summary").first();
    await expect(recordSummary).toContainText("Priya Nair");
    await expect(recordSummary).toContainText("Director, Clinic Operations");
    await expect(recordSummary).toContainText("Clinic Operations");
    await expect(recordSummary).toContainText("Olivia Bennett");
  });

  test("typing in 'termination type' does NOT bleed into other fields (collision regression)", async ({
    page,
  }) => {
    await page.getByRole("button", { name: "Open HCM assistant" }).click();
    await page
      .getByRole("textbox", { name: "Describe what you'd like to do" })
      .fill("start a termination");
    await page.getByRole("button", { name: "Go", exact: true }).click();
    await expect(page.getByRole("button", { name: "Close assistant" })).toBeVisible();
    await page.keyboard.press("Escape");

    const businessReason = page.getByRole("textbox", { name: "Business reason" });
    await businessReason.fill("Voluntary departure approved by manager.");
    const effectiveDate = page.locator("input#effectiveAt");
    await effectiveDate.fill("2026-07-01");

    // The textarea contents must NOT have been overwritten by the date field.
    await expect(businessReason).toHaveValue(
      "Voluntary departure approved by manager.",
    );
    await expect(effectiveDate).toHaveValue("2026-07-01");
  });
});
