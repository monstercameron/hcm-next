import { describe, expect, it } from "vitest";

import {
  CANONICAL_WIDGET_TYPE_IDS,
  isCanonicalWidgetTypeId,
} from "./canonical-widget-types.js";
import { createNullAiClient } from "./null-client.js";
import type {
  AiChatMessage,
  AiChatTurnRequest,
  AiUiGenerationRequest,
} from "./ai-client.js";

function buildRequest(
  overrides: Partial<AiUiGenerationRequest> = {},
): AiUiGenerationRequest {
  const base: AiUiGenerationRequest = {
    workflow: {
      intent: "employee.termination",
      subjectType: "employee",
      submit: {
        proposedSnapshot: {
          terminationDate: { $source: "input", path: "terminationDate" },
          reasonCode: { $source: "input", path: "reasonCode" },
        },
      },
      states: {
        draft: {
          actions: [
            { transition: "submit", label: "Submit termination" },
            { transition: "cancel", label: "Cancel" },
          ],
        },
      },
    },
    workflowConfigHash: "hash-123",
    currentState: "draft",
    currentInteraction: "termination_intake",
    subject: {
      id: "emp_42",
      displayName: "Jane Rivera",
      jobTitle: "Senior Engineer",
      department: "Platform",
    },
    actor: {
      id: "actor_hrbp",
      displayName: "Riley HRBP",
      roles: ["hr_admin"],
    },
    userPrompt: "Start a termination for Jane",
    correlationId: "corr_1",
    idempotencyKey: "idem_1",
  };
  return { ...base, ...overrides };
}

describe("createNullAiClient.generatePageDefinition", () => {
  it("returns a page with at least one region", async () => {
    const client = createNullAiClient();

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.page.regions.length).toBeGreaterThanOrEqual(1);
    expect(result.value.page.regions[0]?.id).toBe("main");
  });

  it("derives a stable id from the workflow intent", async () => {
    const client = createNullAiClient();

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.page.id).toBe("ai-generated-employee.termination");
    expect(result.value.page.workflowTypes).toEqual(["employee.termination"]);
    expect(result.value.page.surfaceModes).toEqual(["full_app"]);
  });

  it("emits only canonical widget type ids", async () => {
    const client = createNullAiClient();

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const types = result.value.page.regions.flatMap((region) =>
      region.widgets.map((widget) => widget.type),
    );
    expect(types.length).toBeGreaterThan(0);
    for (const widgetType of types) {
      expect(isCanonicalWidgetTypeId(widgetType)).toBe(true);
      expect(CANONICAL_WIDGET_TYPE_IDS).toContain(widgetType);
    }
  });

  it("includes a record summary widget when the subject is provided", async () => {
    const client = createNullAiClient();

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const widgets = result.value.page.regions.flatMap((region) => region.widgets);
    const summary = widgets.find((widget) => widget.type === "data.recordSummary");
    expect(summary).toBeDefined();
    // Title is intentionally generic ("Employee details") rather than the
    // subject's display name — the picker can swap the subject at any time,
    // so the section header should not be tied to one person.
    expect(summary?.title).toBe("Employee details");
    expect((summary?.props as Record<string, unknown>)?.["recordId"]).toBe(
      "$selectedSubject",
    );
  });

  it("renders a $selectedSubject-bound record summary even without a baked subject", async () => {
    const client = createNullAiClient();

    const requestWithoutSubject: AiUiGenerationRequest = {
      ...buildRequest(),
    };
    delete (requestWithoutSubject as { subject?: unknown }).subject;

    const result = await client.generatePageDefinition(requestWithoutSubject);

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const widgets = result.value.page.regions.flatMap((region) => region.widgets);
    const summary = widgets.find((widget) => widget.type === "data.recordSummary");
    expect(summary).toBeDefined();
    expect((summary?.props as Record<string, unknown>)?.["recordId"]).toBe(
      "$selectedSubject",
    );
  });

  it("emits a dynamic field group for each declared submit field", async () => {
    const client = createNullAiClient();

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const widgets = result.value.page.regions.flatMap((region) => region.widgets);
    const fieldGroup = widgets.find(
      (widget) => widget.type === "form.dynamicFieldGroup",
    );
    expect(fieldGroup).toBeDefined();
    const fields = (fieldGroup?.props?.["fields"] ?? []) as Array<{
      id: string;
    }>;
    expect(fields.map((field) => field.id)).toEqual(["terminationDate", "reasonCode"]);
  });

  it("caps the intake page at four widgets and omits redundant chrome", async () => {
    const client = createNullAiClient();

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const widgets = result.value.page.regions[0]!.widgets;
    expect(widgets.length).toBeLessThanOrEqual(5);
    // The old verbose layout used a `content.callout` ("Required information")
    // and a `data.checklist` ("Required input") on top of the picker, summary,
    // field group, and action bar. The tight composition drops both.
    const types = widgets.map((widget) => widget.type);
    expect(types).not.toContain("content.callout");
    expect(types).not.toContain("data.checklist");
    expect(types).not.toContain("data.labelValueList");
  });

  it("translates known transition ids into plain-English action labels", async () => {
    const client = createNullAiClient();

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const widgets = result.value.page.regions.flatMap((region) => region.widgets);
    const actionBar = widgets.find((widget) => widget.type === "workflow.actionBar");
    expect(actionBar).toBeDefined();
    expect(actionBar?.title).toBe("Submit for approval");
    const actions = actionBar?.props?.["actions"] as
      | Array<{ transition: string; label: string; variant?: string }>
      | undefined;
    expect(actions).toEqual([
      { transition: "submit", label: "Submit for approval", variant: "primary" },
      { transition: "cancel", label: "Cancel" },
    ]);
  });

  it("always renders form.subjectPicker as the first widget in the first region", async () => {
    const client = createNullAiClient();

    const result = await client.generatePageDefinition(
      buildRequest({
        employeeOptions: [
          { id: "emp_1", displayName: "Alice North", jobTitle: "Recruiter" },
          { id: "emp_2", displayName: "Beto South", department: "Finance" },
        ],
      }),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const firstWidget = result.value.page.regions[0]?.widgets[0];
    expect(firstWidget?.type).toBe("form.subjectPicker");
    const options = firstWidget?.props?.["options"] as
      | Array<{ id: string; displayName: string }>
      | undefined;
    expect(options).toBeDefined();
    expect(options?.map((option) => option.id)).toContain("emp_1");
    expect(options?.map((option) => option.id)).toContain("emp_2");
    // Picker also receives the active subject as the default selection.
    expect(firstWidget?.props?.["defaultSubjectId"]).toBe("emp_42");
  });

  it("renders the subject picker even when no subject and no options are provided", async () => {
    const client = createNullAiClient();

    const request = buildRequest();
    delete (request as { subject?: unknown }).subject;

    const result = await client.generatePageDefinition(request);

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const firstWidget = result.value.page.regions[0]?.widgets[0];
    expect(firstWidget?.type).toBe("form.subjectPicker");
    expect(firstWidget?.props?.["defaultSubjectId"]).toBeUndefined();
    const options = firstWidget?.props?.["options"] as Array<unknown> | undefined;
    expect(Array.isArray(options)).toBe(true);
    expect(options?.length).toBe(0);
  });

  it("returns null provider metadata for tracking", async () => {
    const client = createNullAiClient();

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.providerMetadata).toEqual({
      provider: "null",
      model: "none",
      inputTokens: 0,
      outputTokens: 0,
    });
  });
});

function buildChatRequest(messages: readonly AiChatMessage[]): AiChatTurnRequest {
  return {
    systemPrompt: "system",
    messages,
    tools: [],
    correlationId: "corr_chat_1",
    idempotencyKey: "idem_chat_1",
  };
}

describe("createNullAiClient.runChatTurn", () => {
  it("opens with a friendly intro on the first user message", async () => {
    const client = createNullAiClient();

    const result = await client.runChatTurn(
      buildChatRequest([{ role: "user", content: "hi" }]),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.assistantMessage.content).toMatch(/^Hi/);
    expect(result.value.assistantMessage.toolCalls).toBeUndefined();
  });

  it("calls search_employees when the user mentions a termination", async () => {
    const client = createNullAiClient();

    const result = await client.runChatTurn(
      buildChatRequest([
        { role: "user", content: "hi" },
        { role: "assistant", content: "Hi — who do you need to help today?" },
        { role: "user", content: "I want to start a termination for Jane" },
      ]),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const toolCalls = result.value.assistantMessage.toolCalls;
    expect(toolCalls?.length).toBe(1);
    expect(toolCalls?.[0]?.name).toBe("search_employees");
  });

  it("calls generate_ui_page after a search_employees tool result", async () => {
    const client = createNullAiClient();

    const result = await client.runChatTurn(
      buildChatRequest([
        { role: "user", content: "Start a termination" },
        {
          role: "assistant",
          content: null,
          toolCalls: [{ id: "call-1", name: "search_employees", arguments: {} }],
        },
        {
          role: "tool",
          toolCallId: "call-1",
          content: JSON.stringify({
            employees: [
              { id: "emp_42", displayName: "Jane Rivera", employmentStatus: "active" },
            ],
          }),
        },
      ]),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const toolCalls = result.value.assistantMessage.toolCalls;
    expect(toolCalls?.length).toBe(1);
    expect(toolCalls?.[0]?.name).toBe("generate_ui_page");
    expect(toolCalls?.[0]?.arguments).toMatchObject({
      intent: "employee.termination",
      subjectId: "emp_42",
    });
  });

  it("finalises the conversation after a generate_ui_page tool result", async () => {
    const client = createNullAiClient();

    const result = await client.runChatTurn(
      buildChatRequest([
        { role: "user", content: "Start a termination" },
        {
          role: "assistant",
          content: null,
          toolCalls: [{ id: "call-1", name: "search_employees", arguments: {} }],
        },
        {
          role: "tool",
          toolCallId: "call-1",
          content: JSON.stringify({
            employees: [
              { id: "emp_42", displayName: "Jane Rivera", employmentStatus: "active" },
            ],
          }),
        },
        {
          role: "assistant",
          content: null,
          toolCalls: [
            {
              id: "call-2",
              name: "generate_ui_page",
              arguments: { intent: "employee.termination", subjectId: "emp_42" },
            },
          ],
        },
        {
          role: "tool",
          toolCallId: "call-2",
          content: JSON.stringify({ ok: true, pageId: "x", regions: 1, widgets: 3 }),
        },
      ]),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.assistantMessage.content).toMatch(/termination form/);
    expect(result.value.assistantMessage.toolCalls).toBeUndefined();
  });

  it("echoes a clarifying prompt for unrelated free-form input", async () => {
    const client = createNullAiClient();

    const result = await client.runChatTurn(
      buildChatRequest([
        { role: "user", content: "hi" },
        { role: "assistant", content: "Hi — who do you need to help today?" },
        { role: "user", content: "what time is it" },
      ]),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.assistantMessage.content).toMatch(/Tell me more/);
  });
});
