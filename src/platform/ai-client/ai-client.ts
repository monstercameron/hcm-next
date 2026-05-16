import type { AppError, Result, StructuredLogger } from "@hcm-next/foundation";

/**
 * Permission-gated input for an AI change review. The caller is responsible
 * for filtering currentState and proposedState to only include fields the
 * acting user is authorized to see before passing them here.
 */
export type AiChangeReviewRequest = {
  changeType: string;
  currentState: Record<string, unknown>;
  proposedState: Record<string, unknown>;
  /** Field names visible to the actor — logged for audit without values. */
  visibleFields: string[];
  workflowInstanceId: string;
  changeRequestId: string;
  correlationId: string;
  idempotencyKey: string;
};

export type AiRiskLevel = "low" | "medium" | "high";

export type AiChangeReview = {
  summary: string;
  riskLevel: AiRiskLevel;
  missingData: string[];
  policyFlags: string[];
  downstreamImpact: string[];
  recommendedNextAction: string;
  providerMetadata: {
    provider: string;
    model: string;
    inputTokens: number;
    outputTokens: number;
  };
};

/**
 * Structural subset of the full WorkflowConfig that the AI UI generator
 * needs. Declared inline so the ai-client package does not depend on the
 * workflows source tree. The full WorkflowConfig type is structurally
 * assignable to this shape at every call site.
 */
export type AiWorkflowConfigInput = {
  readonly intent: string;
  readonly subjectType?: string;
  readonly metadata?: {
    readonly title?: string;
    readonly description?: string;
    readonly domain?: string;
  } & Record<string, unknown>;
  readonly interactions?: Readonly<Record<string, Readonly<Record<string, unknown>>>>;
  readonly states?: Readonly<
    Record<
      string,
      {
        readonly actions: ReadonlyArray<{
          readonly transition: string;
          readonly label: string;
          readonly actor?: string;
          readonly handler?: string;
        }>;
      }
    >
  >;
  readonly submit?: {
    readonly proposedSnapshot?: Readonly<Record<string, unknown>>;
    readonly currentSnapshot?: Readonly<Record<string, unknown>>;
  } & Record<string, unknown>;
} & Record<string, unknown>;

/**
 * Subject summary the AI uses to anchor the generated page to a specific
 * employee record. Only display-safe identifiers and roles are included.
 */
export type AiUiGenerationSubject = {
  id: string;
  displayName: string;
  jobTitle?: string;
  department?: string;
  /** Display name of the worker's manager, if one is set. */
  manager?: string;
};

/**
 * Actor summary used so the AI can scope action options to the requester's
 * authority. Roles are role keys, not full permission lists.
 */
export type AiUiGenerationActor = {
  id: string;
  displayName: string;
  roles: readonly string[];
};

/**
 * Provider-agnostic request for a generated workflow page. The caller has
 * already loaded and hashed the workflow config, applied permission filters,
 * and resolved the subject and actor projections.
 *
 * `employeeOptions` carries the candidate employees the AI may surface in the
 * page's subject picker. Callers populate it with the same shape the
 * `form.subjectPicker` widget consumes; an empty array tells the AI no
 * picker should be rendered.
 */
export type AiUiGenerationRequest = {
  workflow: AiWorkflowConfigInput;
  workflowConfigHash: string;
  currentState?: string;
  currentInteraction?: string;
  subject?: AiUiGenerationSubject;
  actor: AiUiGenerationActor;
  userPrompt: string;
  correlationId: string;
  idempotencyKey: string;
  employeeOptions?: readonly AiUiGenerationSubject[];
};

/**
 * Page definition produced by the AI alongside the provider metadata used
 * for billing attribution and audit. The shape of `page` mirrors the
 * platform PageDefinition contract.
 */
export type AiUiGenerationResult = {
  page: AiGeneratedPageDefinition;
  providerMetadata: {
    provider: string;
    model: string;
    inputTokens: number;
    outputTokens: number;
  };
};

/**
 * Mirror of the platform PageDefinition shape kept inline so the ai-client
 * package does not depend on ui-contracts at the type level. The platform
 * PageDefinition is structurally assignable to this shape — callers can
 * narrow back to the canonical type.
 */
export type AiGeneratedPageDefinition = {
  id: string;
  title: string;
  description: string;
  workflowTypes: readonly string[];
  surfaceModes: readonly string[];
  supportedStates?: readonly string[];
  actorRoles?: readonly string[];
  regions: readonly AiGeneratedPageRegion[];
};

export type AiGeneratedPageRegion = {
  id: string;
  title?: string;
  layout: "stack" | "grid" | "split" | "sidebar" | "sticky_rail";
  width: "narrow" | "content" | "wide" | "full";
  widgets: readonly AiGeneratedWidgetInstance[];
};

export type AiGeneratedWidgetInstance = {
  id: string;
  type: string;
  title?: string;
  description?: string;
  size?: "compact" | "half" | "full" | "wide";
  props?: Readonly<Record<string, unknown>>;
};

/**
 * Role identifier for a single message in an AI chat conversation. Mirrors the
 * OpenAI Chat Completions role vocabulary so adapter mapping is trivial.
 */
export type AiChatRole = "user" | "assistant" | "system" | "tool";

/**
 * A single tool call requested by the assistant. `arguments` is already a
 * parsed object — adapters take care of deserialising whatever the provider
 * returned (JSON string for OpenAI, etc.).
 */
export type AiChatToolCall = {
  id: string;
  name: string;
  arguments: Record<string, unknown>;
};

/**
 * One message in a chat history. The assistant variant may carry either text
 * content, a list of tool calls, or both; the tool variant carries the result
 * of a previously requested tool call keyed by `toolCallId`.
 */
export type AiChatMessage =
  | { role: "system"; content: string }
  | { role: "user"; content: string }
  | { role: "assistant"; content: string | null; toolCalls?: readonly AiChatToolCall[] }
  | { role: "tool"; toolCallId: string; content: string };

/**
 * Tool the assistant may invoke during a chat turn. The schema is passed to
 * the provider so it can validate `arguments` before returning them.
 */
export type AiChatToolDefinition = {
  name: string;
  description: string;
  parametersJsonSchema: Record<string, unknown>;
};

/**
 * Request shape for a single chat turn. Caller is responsible for building the
 * system prompt, supplying the conversation messages, and registering the
 * tools the assistant is permitted to invoke.
 */
export type AiChatTurnRequest = {
  systemPrompt: string;
  messages: readonly AiChatMessage[];
  tools: readonly AiChatToolDefinition[];
  correlationId: string;
  idempotencyKey: string;
};

/**
 * Response shape for a single chat turn. The assistant message either carries
 * a final text reply or one or more tool calls the caller must execute and
 * feed back as `tool` messages on the next turn.
 */
export type AiChatTurnResponse = {
  assistantMessage: {
    content: string | null;
    toolCalls?: readonly AiChatToolCall[];
  };
  providerMetadata: {
    provider: string;
    model: string;
    inputTokens: number;
    outputTokens: number;
  };
};

/**
 * Provider-agnostic AI client. Implementations wrap a specific inference
 * provider (OpenAI, Anthropic, Azure OpenAI, etc.) behind this interface.
 * Callers never depend on provider SDK types.
 */
export type AiClient = {
  generateChangeReview(
    request: AiChangeReviewRequest,
    logger?: StructuredLogger,
  ): Promise<Result<AiChangeReview, AppError>>;
  generatePageDefinition(
    request: AiUiGenerationRequest,
    logger?: StructuredLogger,
  ): Promise<Result<AiUiGenerationResult, AppError>>;
  runChatTurn(
    request: AiChatTurnRequest,
    logger?: StructuredLogger,
  ): Promise<Result<AiChatTurnResponse, AppError>>;
};
