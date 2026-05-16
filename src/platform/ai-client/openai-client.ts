import OpenAI from "openai";
import {
  aiReviewError,
  aiUiGenerationError,
  err,
  fromPromise,
  fromThrowable,
  ok,
  type StructuredLogger,
} from "@hcm-next/foundation";
import type {
  AiChangeReview,
  AiChangeReviewRequest,
  AiChatMessage,
  AiChatToolCall,
  AiChatTurnRequest,
  AiChatTurnResponse,
  AiClient,
  AiGeneratedPageDefinition,
  AiGeneratedPageRegion,
  AiGeneratedWidgetInstance,
  AiRiskLevel,
  AiUiGenerationRequest,
  AiUiGenerationResult,
} from "./ai-client.js";
import { CANONICAL_WIDGET_TYPE_IDS } from "./canonical-widget-types.js";

export type OpenAiClientConfig = {
  apiKey: string;
  model?: string;
  /** Organization ID for API billing attribution. */
  organization?: string;
};

const REVIEW_JSON_SCHEMA = {
  type: "object",
  properties: {
    summary: {
      type: "string",
      description: "Plain-English summary of what is changing and why it matters.",
    },
    riskLevel: {
      type: "string",
      enum: ["low", "medium", "high"],
      description: "Overall risk level of this change.",
    },
    missingData: {
      type: "array",
      items: { type: "string" },
      description:
        "Field names or data points that are absent but required for a complete review.",
    },
    policyFlags: {
      type: "array",
      items: { type: "string" },
      description: "Policy rules or constraints that this change may conflict with.",
    },
    downstreamImpact: {
      type: "array",
      items: { type: "string" },
      description:
        "Systems, records, or processes that will be affected by this change.",
    },
    recommendedNextAction: {
      type: "string",
      description: "The single most important next step for the approver or requester.",
    },
  },
  required: [
    "summary",
    "riskLevel",
    "missingData",
    "policyFlags",
    "downstreamImpact",
    "recommendedNextAction",
  ],
  additionalProperties: false,
} as const;

const SYSTEM_PROMPT =
  "You are an AI assistant specialized in reviewing proposed HR data changes for enterprise organizations. " +
  "You analyze only the data provided — you have no access to external systems. " +
  "Be objective, precise, and concise. Flag real risks; do not invent concerns.";

const PAGE_REGION_LAYOUTS = [
  "stack",
  "grid",
  "split",
  "sidebar",
  "sticky_rail",
] as const;

const PAGE_REGION_WIDTHS = ["narrow", "content", "wide", "full"] as const;

const PAGE_SURFACE_MODES = [
  "full_app",
  "customer_portal",
  "embedded_manager_widget",
  "embedded_approval_widget",
  "employee_self_service",
  "hrbp_workbench",
  "compensation_review",
  "payroll_review",
  "finance_review",
  "admin_preview",
  "audit_export",
  "mobile_compact",
  "email_summary",
] as const;

const WIDGET_SIZES = ["compact", "half", "full", "wide"] as const;

const PAGE_DEFINITION_JSON_SCHEMA = {
  type: "object",
  properties: {
    id: { type: "string", description: "Stable id for the generated page." },
    title: { type: "string", description: "Title shown at the top of the page." },
    description: {
      type: "string",
      description: "Short description rendered under the title.",
    },
    workflowTypes: {
      type: "array",
      items: { type: "string" },
      description: "Workflow intents this page is appropriate for.",
    },
    surfaceModes: {
      type: "array",
      items: { type: "string", enum: PAGE_SURFACE_MODES },
      description: "Surfaces this page is intended to render on.",
    },
    regions: {
      type: "array",
      minItems: 1,
      items: {
        type: "object",
        properties: {
          id: { type: "string" },
          title: { type: "string" },
          layout: { type: "string", enum: PAGE_REGION_LAYOUTS },
          width: { type: "string", enum: PAGE_REGION_WIDTHS },
          widgets: {
            type: "array",
            items: {
              type: "object",
              properties: {
                id: { type: "string" },
                type: {
                  type: "string",
                  enum: CANONICAL_WIDGET_TYPE_IDS,
                  description:
                    "Widget type id. Must be one of the canonical platform widget ids.",
                },
                title: { type: "string" },
                description: { type: "string" },
                size: { type: "string", enum: WIDGET_SIZES },
                props: {
                  type: "object",
                  description: "Widget-specific configuration props.",
                  additionalProperties: true,
                },
              },
              required: ["id", "type"],
              additionalProperties: false,
            },
          },
        },
        required: ["id", "layout", "width", "widgets"],
        additionalProperties: false,
      },
    },
  },
  required: ["id", "title", "description", "workflowTypes", "surfaceModes", "regions"],
  additionalProperties: false,
} as const;

const UI_GENERATION_SYSTEM_PROMPT =
  "You are an HR coordinator assistant inside HCM Next. " +
  "Your job is to lay out the screen that lets the user complete the current workflow state. " +
  "Speak in practical, plain language. Do not mention JSON, schemas, models, or that an assistant generated the page. " +
  "Compose the screen using only the widgets and fields available in the workflow you are given. " +
  "Grounding rule: every field, action, and entity reference on the page must trace back to a value " +
  "present in the workflow you receive. Do not invent fields, actions, or entities that the workflow does not declare. " +
  "Pick widget types only from the supplied canonical widget vocabulary. Choose layouts that match how an " +
  "HR coordinator would actually work the task: summary up top, supporting context next, action bar at the end. " +
  "Keep titles short. Keep descriptions one sentence. " +
  // -- HR-coordinator voice --
  "Speak in HR coordinator language. Never expose internal identifiers (role tokens like `hr_admin`, " +
  "transition names like `submit_input`, block names like `system.employee_data.*`, intent names like " +
  '`employee.termination`). Translate them to plain English: `hr_admin` -> "HR Director", ' +
  '`hr_termination_review` -> "Termination review", `system.employee_data.termination.preflight` -> ' +
  '"Compliance check", and so on. ' +
  'Do not generate a "Process facts" widget that lists block names, transition ids, or preflight ' +
  "identifiers. Those are implementation details the user does not care about. " +
  "Widget titles must be short, human, and business-meaningful. Examples: " +
  'for the subject picker use "Employee" (not "Select employee" - the field label already says that); ' +
  'for the record summary use "Employee details" (not "Selected employee"); ' +
  'for the input form use a workflow-specific noun phrase like "Termination details" (not "Workflow input"); ' +
  'for the action bar use a verb phrase like "Submit for approval" (not "Action Bar" or "Submit action bar"). ' +
  'For the action bar\'s title and the submit button label, never include the words "action bar"; use a verb-phrase ' +
  'like "Submit termination" / "Submit for approval" / "Cancel". ' +
  // -- Tighter page composition --
  "Generate a SHORT, focused page. Use AT MOST 4-5 widgets per page unless the workflow specifically requires more. " +
  "For an intake form, the standard composition is exactly four widgets in this order: " +
  "(1) `form.subjectPicker`, (2) `data.recordSummary` bound to the selected subject, " +
  "(3) `form.dynamicFieldGroup` for the workflow's submit fields, (4) `workflow.actionBar` with the submit/cancel actions. " +
  'Do NOT add a redundant `data.checklist` of "required input" (the field group already shows required-field tags). ' +
  'Do NOT add a `data.labelValueList` of "key facts" (the employee summary already shows job and department). ' +
  'Do NOT add an "About this request" `content.callout` (the user already knows what they are doing). ' +
  // -- Picker / summary / action wiring (unchanged) --
  "ALWAYS include a `form.subjectPicker` widget as the first widget in the first region. " +
  "Populate its `options` prop from the `Employee options` list provided in the user prompt. " +
  "If a subject was provided, set `defaultSubjectId` to that subject's id so the picker pre-selects it; otherwise leave it empty. " +
  'Set `label` to a short instruction such as "Select employee to apply this workflow to" and `required` to true. ' +
  "Never omit the picker - the user relies on it to switch the workflow's subject visually. " +
  'For the `data.recordSummary` widget that displays the picked subject, set `props.recordId = "$selectedSubject"` and do NOT set `props.record`. The runtime will populate it from the picker\'s selection. ' +
  "For the primary submit button in `workflow.actionBar`, set the action's `transition` to one of the workflow's declared submit transitions (use the first action listed in `states[currentState].actions`) so the runtime can dispatch it correctly. Set `variant` to `\"primary\"`. " +
  'When rendering the action\'s label, translate the transition id to plain English (e.g. `submit_input` -> "Submit for approval").';

type OpenAiChatToolCallWire = {
  id: string;
  type: "function";
  function: { name: string; arguments: string };
};

type OpenAiChatMessageWire =
  | { role: "system"; content: string }
  | { role: "user"; content: string }
  | {
      role: "assistant";
      content: string | null;
      tool_calls?: OpenAiChatToolCallWire[];
    }
  | { role: "tool"; tool_call_id: string; content: string };

type OpenAiChatToolWire = {
  type: "function";
  function: {
    name: string;
    description: string;
    parameters: Record<string, unknown>;
  };
};

type OpenAiChatCompletionWire = {
  choices?: Array<{
    message?: {
      content?: string | null;
      tool_calls?: OpenAiChatToolCallWire[];
    };
  }>;
  usage?: {
    prompt_tokens?: number;
    completion_tokens?: number;
  };
};

function buildOpenAiChatMessages(request: AiChatTurnRequest): OpenAiChatMessageWire[] {
  const messages: OpenAiChatMessageWire[] = [
    { role: "system", content: request.systemPrompt },
  ];
  for (const message of request.messages) {
    messages.push(toOpenAiChatMessage(message));
  }
  return messages;
}

function toOpenAiChatMessage(message: AiChatMessage): OpenAiChatMessageWire {
  if (message.role === "system") {
    return { role: "system", content: message.content };
  }
  if (message.role === "user") {
    return { role: "user", content: message.content };
  }
  if (message.role === "tool") {
    return {
      role: "tool",
      tool_call_id: message.toolCallId,
      content: message.content,
    };
  }
  const toolCalls = message.toolCalls;
  if (toolCalls === undefined || toolCalls.length === 0) {
    return { role: "assistant", content: message.content };
  }
  return {
    role: "assistant",
    content: message.content,
    tool_calls: toolCalls.map((call) => ({
      id: call.id,
      type: "function",
      function: {
        name: call.name,
        arguments: JSON.stringify(call.arguments ?? {}),
      },
    })),
  };
}

function buildOpenAiChatTools(request: AiChatTurnRequest): OpenAiChatToolWire[] {
  return request.tools.map((tool) => ({
    type: "function",
    function: {
      name: tool.name,
      description: tool.description,
      parameters: tool.parametersJsonSchema,
    },
  }));
}

function mapOpenAiToolCalls(
  toolCalls: OpenAiChatToolCallWire[] | undefined,
): { ok: true; value: AiChatToolCall[] } | { ok: false; error: Error } {
  if (toolCalls === undefined || toolCalls.length === 0) {
    return { ok: true, value: [] };
  }
  const result: AiChatToolCall[] = [];
  for (const call of toolCalls) {
    if (call.type !== "function") {
      continue;
    }
    const argsText = call.function.arguments ?? "{}";
    const parsed = fromThrowable(
      () => JSON.parse(argsText) as unknown,
      (cause) =>
        cause instanceof Error ? cause : new Error("tool_call_arguments_parse_failed"),
    );
    if (!parsed.ok) {
      return { ok: false, error: parsed.error };
    }
    const argsValue = parsed.value;
    const argsObject: Record<string, unknown> =
      typeof argsValue === "object" && argsValue !== null && !Array.isArray(argsValue)
        ? (argsValue as Record<string, unknown>)
        : {};
    result.push({
      id: call.id,
      name: call.function.name,
      arguments: argsObject,
    });
  }
  return { ok: true, value: result };
}

function buildUserPrompt(request: AiChangeReviewRequest): string {
  return (
    `Change type: ${request.changeType}\n` +
    `Workflow instance: ${request.workflowInstanceId}\n\n` +
    `Current state:\n${JSON.stringify(request.currentState, null, 2)}\n\n` +
    `Proposed state:\n${JSON.stringify(request.proposedState, null, 2)}\n\n` +
    `Authorized visible fields: ${request.visibleFields.join(", ")}`
  );
}

function buildUiGenerationUserPrompt(request: AiUiGenerationRequest): string {
  const subjectLines =
    request.subject === undefined
      ? "Subject: (none provided)"
      : [
          `Subject id: ${request.subject.id}`,
          `Subject display name: ${request.subject.displayName}`,
          ...(request.subject.jobTitle === undefined
            ? []
            : [`Subject job title: ${request.subject.jobTitle}`]),
          ...(request.subject.department === undefined
            ? []
            : [`Subject department: ${request.subject.department}`]),
        ].join("\n");

  const employeeOptions = request.employeeOptions ?? [];
  const employeeOptionsBlock =
    employeeOptions.length === 0
      ? "Employee options: (none provided)"
      : `Employee options (use these in form.subjectPicker.options):\n${JSON.stringify(
          employeeOptions,
          null,
          2,
        )}`;

  return (
    `Workflow intent: ${request.workflow.intent}\n` +
    `Current state: ${request.currentState ?? "(none provided)"}\n` +
    `Current interaction: ${request.currentInteraction ?? "(none provided)"}\n` +
    `${subjectLines}\n` +
    `Actor display name: ${request.actor.displayName}\n` +
    `Actor roles: ${request.actor.roles.join(", ") || "(none)"}\n\n` +
    `${employeeOptionsBlock}\n\n` +
    `User prompt: ${request.userPrompt}\n\n` +
    `Workflow configuration JSON:\n${JSON.stringify(request.workflow, null, 2)}`
  );
}

function validateRiskLevel(value: unknown): AiRiskLevel {
  if (value === "low" || value === "medium" || value === "high") {
    return value;
  }
  return "medium";
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((entry) => typeof entry === "string");
}

function validateWidgetInstance(value: unknown): AiGeneratedWidgetInstance | undefined {
  if (typeof value !== "object" || value === null) {
    return undefined;
  }

  const record = value as Record<string, unknown>;

  if (typeof record["id"] !== "string" || typeof record["type"] !== "string") {
    return undefined;
  }

  const widgetType = record["type"];

  if (!CANONICAL_WIDGET_TYPE_IDS.includes(widgetType as never)) {
    return undefined;
  }

  const widget: AiGeneratedWidgetInstance = {
    id: record["id"],
    type: widgetType,
  };

  if (typeof record["title"] === "string") {
    widget.title = record["title"];
  }
  if (typeof record["description"] === "string") {
    widget.description = record["description"];
  }
  const candidateSize = record["size"];
  if (
    typeof candidateSize === "string" &&
    (WIDGET_SIZES as readonly string[]).includes(candidateSize)
  ) {
    widget.size = candidateSize as (typeof WIDGET_SIZES)[number];
  }
  if (typeof record["props"] === "object" && record["props"] !== null) {
    widget.props = record["props"] as Readonly<Record<string, unknown>>;
  }

  return widget;
}

function validateRegion(value: unknown): AiGeneratedPageRegion | undefined {
  if (typeof value !== "object" || value === null) {
    return undefined;
  }

  const record = value as Record<string, unknown>;

  if (
    typeof record["id"] !== "string" ||
    typeof record["layout"] !== "string" ||
    typeof record["width"] !== "string" ||
    !(PAGE_REGION_LAYOUTS as readonly string[]).includes(record["layout"]) ||
    !(PAGE_REGION_WIDTHS as readonly string[]).includes(record["width"]) ||
    !Array.isArray(record["widgets"])
  ) {
    return undefined;
  }

  const widgets: AiGeneratedWidgetInstance[] = [];
  for (const candidate of record["widgets"]) {
    const widget = validateWidgetInstance(candidate);
    if (widget === undefined) {
      return undefined;
    }
    widgets.push(widget);
  }

  const region: AiGeneratedPageRegion = {
    id: record["id"],
    layout: record["layout"] as AiGeneratedPageRegion["layout"],
    width: record["width"] as AiGeneratedPageRegion["width"],
    widgets,
  };

  if (typeof record["title"] === "string") {
    region.title = record["title"];
  }

  return region;
}

function validatePageDefinition(
  value: Record<string, unknown>,
): AiGeneratedPageDefinition | undefined {
  if (
    typeof value["id"] !== "string" ||
    typeof value["title"] !== "string" ||
    typeof value["description"] !== "string" ||
    !isStringArray(value["workflowTypes"]) ||
    !isStringArray(value["surfaceModes"]) ||
    !Array.isArray(value["regions"]) ||
    value["regions"].length === 0
  ) {
    return undefined;
  }

  const regions: AiGeneratedPageRegion[] = [];
  for (const candidate of value["regions"]) {
    const region = validateRegion(candidate);
    if (region === undefined) {
      return undefined;
    }
    regions.push(region);
  }

  const page: AiGeneratedPageDefinition = {
    id: value["id"],
    title: value["title"],
    description: value["description"],
    workflowTypes: value["workflowTypes"],
    surfaceModes: value["surfaceModes"],
    regions,
  };

  if (isStringArray(value["supportedStates"])) {
    page.supportedStates = value["supportedStates"];
  }
  if (isStringArray(value["actorRoles"])) {
    page.actorRoles = value["actorRoles"];
  }

  return page;
}

/**
 * Creates an AiClient backed by the OpenAI Responses API.
 * Structured output is enforced via json_schema so the response is always
 * parseable without prompt-engineering fragility.
 */
export function createOpenAiClient(config: OpenAiClientConfig): AiClient {
  const openai = new OpenAI({
    apiKey: config.apiKey,
    ...(config.organization !== undefined ? { organization: config.organization } : {}),
  });
  const model = config.model ?? "gpt-4o";

  return {
    async generateChangeReview(
      request: AiChangeReviewRequest,
      logger?: StructuredLogger,
    ) {
      const startMs = Date.now();

      logger?.info("ai change review started", {
        changeType: request.changeType,
        workflowInstanceId: request.workflowInstanceId,
        changeRequestId: request.changeRequestId,
        visibleFieldCount: request.visibleFields.length,
        model,
        provider: "openai",
      });

      const responseResult = await fromPromise(
        () =>
          openai.responses.create({
            model,
            input: [
              { role: "system", content: SYSTEM_PROMPT },
              { role: "user", content: buildUserPrompt(request) },
            ],
            text: {
              format: {
                type: "json_schema",
                name: "ai_change_review",
                strict: true,
                schema: REVIEW_JSON_SCHEMA,
              },
            },
          }),
        (cause) =>
          aiReviewError({ model, provider: "openai", stage: "api_call" }, cause),
      );

      if (!responseResult.ok) {
        logger?.error("ai change review api call failed", {
          changeType: request.changeType,
          workflowInstanceId: request.workflowInstanceId,
          errorCode: responseResult.error.code,
          durationMs: Date.now() - startMs,
        });
        return responseResult;
      }

      const rawText = responseResult.value.output_text;

      const parseResult = fromThrowable(
        () => JSON.parse(rawText) as Record<string, unknown>,
        (cause) =>
          aiReviewError({ model, provider: "openai", stage: "response_parse" }, cause),
      );

      if (!parseResult.ok) {
        logger?.error("ai change review response parse failed", {
          changeType: request.changeType,
          workflowInstanceId: request.workflowInstanceId,
          errorCode: parseResult.error.code,
          durationMs: Date.now() - startMs,
        });
        return parseResult;
      }

      const parsed = parseResult.value;
      const usage = responseResult.value.usage;

      const review: AiChangeReview = {
        summary: String(parsed["summary"] ?? ""),
        riskLevel: validateRiskLevel(parsed["riskLevel"]),
        missingData: Array.isArray(parsed["missingData"])
          ? (parsed["missingData"] as string[])
          : [],
        policyFlags: Array.isArray(parsed["policyFlags"])
          ? (parsed["policyFlags"] as string[])
          : [],
        downstreamImpact: Array.isArray(parsed["downstreamImpact"])
          ? (parsed["downstreamImpact"] as string[])
          : [],
        recommendedNextAction: String(parsed["recommendedNextAction"] ?? ""),
        providerMetadata: {
          provider: "openai",
          model,
          inputTokens: usage?.input_tokens ?? 0,
          outputTokens: usage?.output_tokens ?? 0,
        },
      };

      logger?.info("ai change review completed", {
        changeType: request.changeType,
        workflowInstanceId: request.workflowInstanceId,
        riskLevel: review.riskLevel,
        inputTokens: review.providerMetadata.inputTokens,
        outputTokens: review.providerMetadata.outputTokens,
        durationMs: Date.now() - startMs,
      });

      return ok(review);
    },

    async generatePageDefinition(
      request: AiUiGenerationRequest,
      logger?: StructuredLogger,
    ) {
      const startMs = Date.now();

      logger?.info("ai ui generation started", {
        intent: request.workflow.intent,
        currentState: request.currentState,
        currentInteraction: request.currentInteraction,
        subjectId: request.subject?.id,
        actorId: request.actor.id,
        actorRoleCount: request.actor.roles.length,
        correlationId: request.correlationId,
        workflowConfigHash: request.workflowConfigHash,
        model,
        provider: "openai",
      });

      const responseResult = await fromPromise(
        () =>
          openai.responses.create({
            model,
            input: [
              { role: "system", content: UI_GENERATION_SYSTEM_PROMPT },
              { role: "user", content: buildUiGenerationUserPrompt(request) },
            ],
            text: {
              format: {
                type: "json_schema",
                name: "ai_page_definition",
                // strict:true requires `additionalProperties:false` on every
                // object, but widget `props` is intentionally open-ended
                // (widget-specific shape). Run in non-strict mode and rely on
                // our own validation downstream.
                strict: false,
                schema: PAGE_DEFINITION_JSON_SCHEMA,
              },
            },
          }),
        (cause) =>
          aiUiGenerationError(
            {
              stage: "api_call",
              model,
              provider: "openai",
              intent: request.workflow.intent,
              correlationId: request.correlationId,
            },
            cause,
          ),
      );

      if (!responseResult.ok) {
        logger?.error("ai ui generation api call failed", {
          intent: request.workflow.intent,
          correlationId: request.correlationId,
          errorCode: responseResult.error.code,
          durationMs: Date.now() - startMs,
        });
        return responseResult;
      }

      const rawText = responseResult.value.output_text;

      const parseResult = fromThrowable(
        () => JSON.parse(rawText) as Record<string, unknown>,
        (cause) =>
          aiUiGenerationError(
            {
              stage: "response_parse",
              model,
              provider: "openai",
              intent: request.workflow.intent,
              correlationId: request.correlationId,
            },
            cause,
          ),
      );

      if (!parseResult.ok) {
        logger?.error("ai ui generation response parse failed", {
          intent: request.workflow.intent,
          correlationId: request.correlationId,
          errorCode: parseResult.error.code,
          durationMs: Date.now() - startMs,
        });
        return parseResult;
      }

      const validated = validatePageDefinition(parseResult.value);

      if (validated === undefined) {
        const validationError = aiUiGenerationError({
          stage: "schema_validation",
          model,
          provider: "openai",
          intent: request.workflow.intent,
          correlationId: request.correlationId,
        });
        logger?.error("ai ui generation schema validation failed", {
          intent: request.workflow.intent,
          correlationId: request.correlationId,
          errorCode: validationError.code,
          durationMs: Date.now() - startMs,
        });
        return err(validationError);
      }

      const usage = responseResult.value.usage;
      const result: AiUiGenerationResult = {
        page: validated,
        providerMetadata: {
          provider: "openai",
          model,
          inputTokens: usage?.input_tokens ?? 0,
          outputTokens: usage?.output_tokens ?? 0,
        },
      };

      logger?.info("ai ui generation completed", {
        intent: request.workflow.intent,
        correlationId: request.correlationId,
        pageId: result.page.id,
        regionCount: result.page.regions.length,
        widgetCount: result.page.regions.reduce(
          (total, region) => total + region.widgets.length,
          0,
        ),
        inputTokens: result.providerMetadata.inputTokens,
        outputTokens: result.providerMetadata.outputTokens,
        durationMs: Date.now() - startMs,
      });

      return ok(result);
    },

    async runChatTurn(request: AiChatTurnRequest, logger?: StructuredLogger) {
      const startMs = Date.now();

      logger?.info("ai chat turn started", {
        correlationId: request.correlationId,
        messageCount: request.messages.length,
        toolCount: request.tools.length,
        model,
        provider: "openai",
      });

      const openAiMessages = buildOpenAiChatMessages(request);
      const openAiTools = buildOpenAiChatTools(request);

      // Cast through unknown because the OpenAI SDK's typed message-param
      // union enforces narrower content shapes per role than the
      // provider-agnostic OpenAiChatMessageWire union we expose internally.
      // The wire shapes are structurally identical.
      const responseResult = await fromPromise(
        async () => {
          const completion = await openai.chat.completions.create({
            model,
            stream: false,
            messages: openAiMessages as never,
            ...(openAiTools.length > 0
              ? { tools: openAiTools as never, tool_choice: "auto" as const }
              : {}),
          });
          return completion as unknown as OpenAiChatCompletionWire;
        },
        (cause) =>
          aiUiGenerationError(
            {
              stage: "chat_turn",
              model,
              provider: "openai",
              correlationId: request.correlationId,
            },
            cause,
          ),
      );

      if (!responseResult.ok) {
        logger?.error("ai chat turn api call failed", {
          correlationId: request.correlationId,
          errorCode: responseResult.error.code,
          durationMs: Date.now() - startMs,
        });
        return responseResult;
      }

      const completion = responseResult.value;
      const choice = completion.choices?.[0];
      if (choice === undefined || choice.message === undefined) {
        const empty = aiUiGenerationError({
          stage: "chat_turn",
          model,
          provider: "openai",
          correlationId: request.correlationId,
          reason: "no_choice_returned",
        });
        logger?.error("ai chat turn returned no choices", {
          correlationId: request.correlationId,
          errorCode: empty.code,
        });
        return err(empty);
      }

      const message = choice.message;
      const toolCallsResult = mapOpenAiToolCalls(message.tool_calls);
      if (!toolCallsResult.ok) {
        logger?.error("ai chat turn tool call args parse failed", {
          correlationId: request.correlationId,
          errorMessage: toolCallsResult.error.message,
        });
        return err(
          aiUiGenerationError(
            {
              stage: "chat_turn",
              model,
              provider: "openai",
              correlationId: request.correlationId,
              reason: "tool_call_args_parse_failed",
            },
            toolCallsResult.error,
          ),
        );
      }
      const toolCalls = toolCallsResult.value;

      const usage = completion.usage;
      const response: AiChatTurnResponse = {
        assistantMessage: {
          content: typeof message.content === "string" ? message.content : null,
          ...(toolCalls.length > 0 ? { toolCalls } : {}),
        },
        providerMetadata: {
          provider: "openai",
          model,
          inputTokens: usage?.prompt_tokens ?? 0,
          outputTokens: usage?.completion_tokens ?? 0,
        },
      };

      logger?.info("ai chat turn completed", {
        correlationId: request.correlationId,
        toolCallCount: toolCalls.length,
        inputTokens: response.providerMetadata.inputTokens,
        outputTokens: response.providerMetadata.outputTokens,
        durationMs: Date.now() - startMs,
      });

      return ok(response);
    },
  };
}
