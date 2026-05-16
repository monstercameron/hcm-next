import { ok } from "@hcm-next/foundation";
import type {
  AiChangeReview,
  AiChatMessage,
  AiChatToolCall,
  AiChatTurnRequest,
  AiChatTurnResponse,
  AiClient,
  AiGeneratedPageDefinition,
  AiGeneratedWidgetInstance,
  AiUiGenerationRequest,
  AiUiGenerationResult,
  AiUiGenerationSubject,
  AiWorkflowConfigInput,
} from "./ai-client.js";

const NULL_REVIEW: AiChangeReview = {
  summary: "AI review is disabled in this environment.",
  riskLevel: "low",
  missingData: [],
  policyFlags: [],
  downstreamImpact: [],
  recommendedNextAction: "Proceed with manual review.",
  providerMetadata: {
    provider: "null",
    model: "none",
    inputTokens: 0,
    outputTokens: 0,
  },
};

function humanizeIntent(intent: string): string {
  return intent
    .split(/[._-]/)
    .filter((part) => part.length > 0)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function listSubmitFieldNames(workflow: AiWorkflowConfigInput): string[] {
  const snapshot =
    workflow.submit?.proposedSnapshot ?? workflow.submit?.currentSnapshot;
  if (snapshot === undefined) {
    return [];
  }
  return Object.keys(snapshot);
}

function listCurrentStateActions(
  workflow: AiWorkflowConfigInput,
  currentState: string | undefined,
): Array<{ transition: string; label: string; variant?: "primary" | "secondary" }> {
  if (currentState === undefined || workflow.states === undefined) {
    return [];
  }
  const stateConfig = workflow.states[currentState];
  if (stateConfig === undefined) {
    return [];
  }
  // First action in the configured state is treated as the submit transition
  // so the runtime can dispatch it from the action bar's primary button.
  return stateConfig.actions.map((action, index) => ({
    transition: action.transition,
    label: action.label,
    ...(index === 0 ? { variant: "primary" as const } : {}),
  }));
}

function buildSubjectPickerWidget(
  request: AiUiGenerationRequest,
): AiGeneratedWidgetInstance {
  // Merge the seed `employeeOptions` with the active `subject` so the picker
  // can pre-select the chosen employee even if the caller didn't include them
  // in the candidate list. Deduplicate by id, keeping the first occurrence.
  const candidates: AiUiGenerationSubject[] = [];
  const seenIds = new Set<string>();
  for (const option of request.employeeOptions ?? []) {
    if (seenIds.has(option.id)) {
      continue;
    }
    seenIds.add(option.id);
    candidates.push(option);
  }
  if (request.subject !== undefined && !seenIds.has(request.subject.id)) {
    candidates.push(request.subject);
  }

  const defaultSubjectId = request.subject?.id;

  return {
    id: "subject-picker",
    type: "form.subjectPicker",
    title: "Employee",
    props: {
      label: "Select employee to apply this workflow to",
      helperText:
        "Pick the employee this workflow should run against. Use search if the person you need isn't listed.",
      required: true,
      options: candidates,
      ...(defaultSubjectId !== undefined ? { defaultSubjectId } : {}),
    },
  };
}

/**
 * Translates an internal transition id (e.g. `submit_input`, `cancel`) into a
 * plain-English button label. Keeps the null client's actions readable for
 * demos and Playwright suites without inventing copy the real workflow does
 * not declare — falls back to a Title Case of the id when no friendly label
 * is known.
 */
function friendlyTransitionLabel(transition: string): string {
  const lookup: Record<string, string> = {
    submit_input: "Submit for approval",
    submit: "Submit for approval",
    cancel: "Cancel",
    cancel_request: "Cancel",
    approve: "Approve",
    reject: "Reject",
  };
  if (lookup[transition] !== undefined) {
    return lookup[transition];
  }
  return transition
    .split(/[._-]/)
    .filter((part) => part.length > 0)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

/**
 * Builds the AI's intake-form composition deterministically: a picker, a
 * summary, an input form, and an action bar — and nothing else. Mirrors the
 * "AT MOST 4-5 widgets" rule the real prompt now enforces so demos and tests
 * exercise the same minimal page the production assistant emits.
 */
function buildPlaceholderPage(
  request: AiUiGenerationRequest,
): AiGeneratedPageDefinition {
  const humanizedIntent = humanizeIntent(request.workflow.intent);
  const widgets: AiGeneratedWidgetInstance[] = [];

  // (1) Picker.
  widgets.push(buildSubjectPickerWidget(request));

  // (2) Record summary bound to the picker selection.
  widgets.push({
    id: "subject-summary",
    type: "data.recordSummary",
    title: "Employee details",
    props: {
      recordId: "$selectedSubject",
      recordType: request.workflow.subjectType ?? "employee",
    },
  });

  // (3) Dynamic field group for the workflow's submit fields. We keep this
  // even when the workflow does not declare submit fields, so the page is
  // structurally identical across intents — the runtime just renders an
  // empty form, which is still cleaner than fabricating a `content.callout`
  // listing implementation details.
  const submitFieldNames = listSubmitFieldNames(request.workflow);
  widgets.push({
    id: "submit-fields",
    type: "form.dynamicFieldGroup",
    title: `${humanizedIntent} details`,
    props: {
      fields: submitFieldNames.map((name) => ({
        id: name,
        type: "text",
        label: humanizeIntent(name),
        required: true,
      })),
    },
  });

  // (4) Action bar. Friendly labels for known transitions; bare-id fallback
  // for unknown ones so the runtime never has to invent copy.
  const stateActions = listCurrentStateActions(request.workflow, request.currentState);
  const friendlyActions = stateActions.map((action) => ({
    ...action,
    label: friendlyTransitionLabel(action.transition),
  }));
  widgets.push({
    id: "workflow-actions",
    type: "workflow.actionBar",
    title: "Submit for approval",
    props: {
      actions: friendlyActions,
    },
  });

  return {
    id: `ai-generated-${request.workflow.intent}`,
    title: humanizedIntent,
    description: `Assistant view for ${humanizedIntent}.`,
    workflowTypes: [request.workflow.intent],
    surfaceModes: ["full_app"],
    regions: [
      {
        id: "main",
        layout: "stack",
        width: "wide",
        widgets,
      },
    ],
  };
}

const NULL_PROVIDER_METADATA = {
  provider: "null",
  model: "none",
  inputTokens: 0,
  outputTokens: 0,
};

/**
 * Deterministic demo state machine for chat turns when no real provider is
 * configured. Branches:
 *
 *   1. The very first user message (no other history) → friendly intro.
 *   2. A user message containing "termination" / "terminate" → tool call
 *      `search_employees` with no query.
 *   3. The most recent message is a `tool` result from `search_employees`
 *      → tool call `generate_ui_page` for `employee.termination` against the
 *      first employee returned.
 *   4. The most recent message is a `tool` result from `generate_ui_page`
 *      → final assistant reply telling the user the form is on the screen.
 *   5. Otherwise → generic clarifying prompt.
 *
 * The branch logic is intentionally minimal — this is a demo stub, not a
 * planner. Production deployments wire `createOpenAiClient` instead.
 */
function runDemoChatTurn(request: AiChatTurnRequest): AiChatTurnResponse {
  const userMessages = request.messages.filter((message) => message.role === "user");
  const lastMessage = request.messages[request.messages.length - 1];

  // Branch 4: forward the renderPage confirmation to the user.
  if (
    lastMessage !== undefined &&
    lastMessage.role === "tool" &&
    looksLikeToolCallForName(
      request.messages,
      lastMessage.toolCallId,
      "generate_ui_page",
    )
  ) {
    return demoAssistantText(
      "I've put the termination form on the screen. Fill it in and submit when ready.",
    );
  }

  // Branch 3: respond to a search_employees result by calling generate_ui_page.
  if (
    lastMessage !== undefined &&
    lastMessage.role === "tool" &&
    looksLikeToolCallForName(
      request.messages,
      lastMessage.toolCallId,
      "search_employees",
    )
  ) {
    const firstEmployeeId = firstEmployeeIdFromToolContent(lastMessage.content);
    if (firstEmployeeId !== undefined) {
      return demoAssistantToolCall({
        id: `null-call-${request.messages.length}`,
        name: "generate_ui_page",
        arguments: {
          intent: "employee.termination",
          subjectId: firstEmployeeId,
        },
      });
    }
    return demoAssistantText(
      "I could not find an employee matching that name. Could you try again?",
    );
  }

  // Branch 1: opening turn — only the first user message exists.
  if (userMessages.length === 1 && request.messages.length === 1) {
    return demoAssistantText(
      "Hi — I'm the HCM assistant. I can help with terminations, contact updates, and approvals. Who do you need to help today?",
    );
  }

  // Branch 2: termination intent — search for the employee.
  const lastUserMessage = userMessages[userMessages.length - 1];
  if (lastUserMessage !== undefined && /terminat/i.test(lastUserMessage.content)) {
    return demoAssistantToolCall({
      id: `null-call-${request.messages.length}`,
      name: "search_employees",
      arguments: {},
    });
  }

  // Branch 5: generic clarifying response.
  return demoAssistantText("Tell me more — what would you like to do?");
}

function demoAssistantText(text: string): AiChatTurnResponse {
  return {
    assistantMessage: { content: text },
    providerMetadata: NULL_PROVIDER_METADATA,
  };
}

function demoAssistantToolCall(toolCall: AiChatToolCall): AiChatTurnResponse {
  return {
    assistantMessage: { content: null, toolCalls: [toolCall] },
    providerMetadata: NULL_PROVIDER_METADATA,
  };
}

function looksLikeToolCallForName(
  messages: readonly AiChatMessage[],
  toolCallId: string,
  name: string,
): boolean {
  for (const message of messages) {
    if (message.role !== "assistant") {
      continue;
    }
    const toolCalls = message.toolCalls;
    if (toolCalls === undefined) {
      continue;
    }
    for (const candidate of toolCalls) {
      if (candidate.id === toolCallId && candidate.name === name) {
        return true;
      }
    }
  }
  return false;
}

function firstEmployeeIdFromToolContent(rawContent: string): string | undefined {
  if (typeof rawContent !== "string" || rawContent.trim().length === 0) {
    return undefined;
  }
  const parsed = tryParseJson(rawContent);
  if (parsed === undefined || typeof parsed !== "object" || parsed === null) {
    return undefined;
  }
  const employees = (parsed as { employees?: unknown }).employees;
  if (!Array.isArray(employees) || employees.length === 0) {
    return undefined;
  }
  const first = employees[0];
  if (typeof first !== "object" || first === null) {
    return undefined;
  }
  const id = (first as { id?: unknown }).id;
  return typeof id === "string" ? id : undefined;
}

function tryParseJson(text: string): unknown {
  // The demo stub deliberately avoids try/catch in business logic by checking
  // for the leading character before JSON.parse — sufficient for the deterministic
  // test corpus this stub is intended to drive.
  const trimmed = text.trim();
  if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) {
    return undefined;
  }
  return JSON.parse(trimmed) as unknown;
}

/**
 * No-op AiClient for test environments and deployments where AI services
 * are intentionally disabled. The page generator returns a deterministic
 * placeholder derived from the workflow JSON so Playwright tests can assert
 * on page structure without calling any provider.
 */
export function createNullAiClient(): AiClient {
  return {
    async generateChangeReview() {
      return ok(NULL_REVIEW);
    },
    async generatePageDefinition(request: AiUiGenerationRequest) {
      const result: AiUiGenerationResult = {
        page: buildPlaceholderPage(request),
        providerMetadata: {
          provider: "null",
          model: "none",
          inputTokens: 0,
          outputTokens: 0,
        },
      };
      return ok(result);
    },
    async runChatTurn(request: AiChatTurnRequest) {
      return ok(runDemoChatTurn(request));
    },
  };
}
