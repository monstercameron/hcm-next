/**
 * Server-resolved action discovery.  Presentation surfaces consume this
 * contract; they do not maintain command registries of their own.
 */

export type ActionScope = "universal" | "contextual";
export type ActionEffect =
  | "non_material"
  | "read"
  | "create"
  | "update"
  | "delete"
  | "execute";
export type ActionRisk = "low" | "medium" | "high" | "critical";
export type UnavailabilityReason =
  | "unauthorized"
  | "unsupported_subject"
  | "missing_context"
  | "missing_capability"
  | "unpublished_intent"
  | "disabled"
  | "stale_version"
  | "not_available";

export interface ActionRoute {
  method: string;
  path: string;
  version: string;
}

export interface ActionVersions {
  feature: string;
  intent: string;
  capability: string;
}

export interface ActionInput {
  name: string;
  type: string;
  required?: boolean;
  redacted?: boolean;
}

export interface ActionDefinition {
  /** Stable, semantic identity. Labels must never be used as identity. */
  semanticId: string;
  label: string;
  description?: string;
  scope: ActionScope;
  featureId: string;
  intentId: string;
  capabilityId: string;
  versions: ActionVersions;
  route: ActionRoute;
  permittedSubjectTypes: readonly string[];
  requiredInputs?: readonly ActionInput[];
  risk: ActionRisk;
  effect: ActionEffect;
  simulationAvailable: boolean;
  enabled?: boolean;
  /** Registry lifecycle gates; omitted means published/available. */
  published?: boolean;
  capabilityAvailable?: boolean;
}

export interface ActionContext {
  subjectType?: string;
  subjectId?: string;
  contextType?: string;
  contextId?: string;
  [key: string]: unknown;
}

export interface AuthorizationDecision {
  allowed: boolean;
  reason?: UnavailabilityReason;
  /** Safe, displayable explanation. Never include policy or subject details. */
  explanation?: string;
}

export type AuthorizeAction = (input: {
  action: ActionDefinition;
  context: ActionContext;
}) => AuthorizationDecision;

export interface DiscoveredAction extends ActionDefinition {
  available: boolean;
  availability: {
    available: boolean;
    reason?: UnavailabilityReason;
    explanation?: string;
  };
  unavailableReason?: UnavailabilityReason;
  unavailableExplanation?: string;
}

export interface DiscoveryRequest {
  context: ActionContext;
  scope?: ActionScope;
  authorize?: AuthorizeAction;
}

const SAFE_REASONS = new Set<UnavailabilityReason>([
  "unauthorized",
  "unsupported_subject",
  "missing_context",
  "missing_capability",
  "unpublished_intent",
  "disabled",
  "stale_version",
  "not_available",
]);

function stableId(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._:-]+/g, "-")
    .replace(/^-|-$/g, "");
}

/** Derives the canonical semantic ID from governed registry coordinates. */
export function createSemanticActionId(
  featureId: string,
  intentId: string,
  capabilityId: string,
): string {
  return `${stableId(featureId)}:${stableId(intentId)}:${stableId(capabilityId)}`;
}

function validateAction(action: ActionDefinition): void {
  if (!action.semanticId || action.semanticId !== stableId(action.semanticId))
    throw new Error(`Invalid semantic action ID: ${action.semanticId}`);
  for (const value of [
    action.featureId,
    action.intentId,
    action.capabilityId,
    action.versions.feature,
    action.versions.intent,
    action.versions.capability,
    action.route.method,
    action.route.path,
    action.route.version,
  ]) {
    if (!value || value.trim() === "")
      throw new Error("Action descriptor contains an empty governed value");
  }
  if (action.permittedSubjectTypes.length === 0)
    throw new Error(`Action ${action.semanticId} has no permitted subject types`);
  if (!/^\/(?:[a-zA-Z0-9._~!$&'()*+,;=:@/-]|\{[^}]+\})+$/.test(action.route.path))
    throw new Error(`Action ${action.semanticId} has an invalid route`);
}

/** Immutable, validated source of all discoverable actions. */
export class ActionRegistry {
  private readonly actions: readonly ActionDefinition[];

  constructor(definitions: readonly ActionDefinition[]) {
    const seen = new Set<string>();
    for (const action of definitions) {
      validateAction(action);
      if (seen.has(action.semanticId))
        throw new Error(`Duplicate semantic action ID: ${action.semanticId}`);
      seen.add(action.semanticId);
    }
    this.actions = definitions.map((action) =>
      Object.freeze({
        ...action,
        permittedSubjectTypes: Object.freeze([...action.permittedSubjectTypes]),
        ...(action.requiredInputs
          ? { requiredInputs: Object.freeze([...action.requiredInputs]) }
          : {}),
      }),
    );
  }

  list(): readonly ActionDefinition[] {
    return this.actions;
  }
  get(semanticId: string): ActionDefinition | undefined {
    return this.actions.find((action) => action.semanticId === semanticId);
  }

  discover(request: DiscoveryRequest): readonly DiscoveredAction[] {
    return this.actions
      .filter((action) => request.scope === undefined || action.scope === request.scope)
      .map((action) => resolveAction(action, request));
  }
}

export function resolveAction(
  action: ActionDefinition,
  request: DiscoveryRequest,
): DiscoveredAction {
  let unavailableReason: UnavailabilityReason | undefined;
  if (action.enabled === false) unavailableReason = "disabled";
  else if (action.published === false) unavailableReason = "unpublished_intent";
  else if (action.capabilityAvailable === false)
    unavailableReason = "missing_capability";
  else if (!action.permittedSubjectTypes.includes(request.context.subjectType ?? ""))
    unavailableReason = "unsupported_subject";
  else if (
    action.scope === "contextual" &&
    (!request.context.contextType || !request.context.contextId)
  )
    unavailableReason = "missing_context";

  let explanation: string | undefined;
  if (unavailableReason === undefined && request.authorize) {
    const decision = request.authorize({ action, context: request.context });
    if (!decision.allowed) {
      unavailableReason = SAFE_REASONS.has(decision.reason ?? "unauthorized")
        ? (decision.reason ?? "unauthorized")
        : "unauthorized";
      explanation = safeExplanation(unavailableReason);
    }
  }
  const available = unavailableReason === undefined;
  return Object.freeze({
    ...action,
    available,
    availability: Object.freeze({
      available,
      ...(unavailableReason ? { reason: unavailableReason } : {}),
      ...(explanation ? { explanation } : {}),
    }),
    ...(unavailableReason
      ? {
          unavailableReason,
          ...(explanation ? { unavailableExplanation: explanation } : {}),
        }
      : {}),
  });
}

function safeExplanation(reason: UnavailabilityReason): string {
  const explanations: Record<UnavailabilityReason, string> = {
    unauthorized: "This action is not available for the current authorization.",
    unsupported_subject: "This action is not available for this subject.",
    missing_context: "This action requires additional context.",
    missing_capability: "This action is not currently enabled.",
    unpublished_intent: "This action is not currently published.",
    disabled: "This action is currently disabled.",
    stale_version: "This action requires a newer version.",
    not_available: "This action is not currently available.",
  };
  return explanations[reason];
}

export function discoverActions(
  registry: ActionRegistry,
  request: DiscoveryRequest,
): readonly DiscoveredAction[] {
  return registry.discover(request);
}
export const resolveActionDiscovery = discoverActions;
