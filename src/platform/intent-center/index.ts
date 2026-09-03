/**
 * UX-007: the Intent Center is a governed read model. It owns no intent,
 * workflow, approval or message truth and never writes to an owner store.
 */

export type IntentReference = {
  intentId: string;
  relationshipId?: string;
  proposalId?: string;
  workItemId?: string;
  messageId?: string;
  workflowId?: string;
  approvalId?: string;
};

export type Authority = {
  principalId: string;
  tenantId: string;
  /** A server-issued authority revision; projections must be rebuilt when it changes. */
  revision: string;
  canView: boolean;
  capabilities: readonly string[];
  /** Optional field/evidence permissions. Missing means deny for restricted data. */
  visibleFields?: readonly string[];
  canViewRestrictedEvidence?: boolean;
};

export type LifecycleState = {
  intent: string;
  work: string;
  approval: string;
  delivery: string;
  external: string;
};

export type SafeAction = {
  id: string;
  capability: string;
  label: string;
  enabled: boolean;
  reason?: "unauthorized" | "stale" | "terminal" | "owner_unavailable" | "redacted";
  /** Exact owner references required to invoke the action. */
  refs: IntentReference;
};

export type RedactedValue = { redacted: true; reason: "restricted" | "not_authorized" };

export type CenterItem = IntentReference & {
  /** Nested copy makes reference propagation explicit for consumers. */
  refs: IntentReference;
  kind: "draft" | "task" | "approval" | "message";
  title: string;
  summary?: string;
  state: LifecycleState;
  stateLabel: string;
  stale: boolean;
  restricted?: boolean;
  evidence?: unknown | RedactedValue;
  actions: readonly SafeAction[];
  deepLink?: DeepLink;
  owner: { principalId?: string; teamId?: string };
  updatedAt: string;
};

export type TimelineEvent = IntentReference & {
  eventId: string;
  occurredAt: string;
  type: string;
  summary: string;
  evidence?: unknown | RedactedValue;
};

export type DeepLink = {
  href: string;
  intentId: string;
  refs: IntentReference;
  authorityRevision: string;
  requiresReauthorization: boolean;
};

export type IntentCenterProjection = {
  authorityRevision: string;
  generatedAt: string;
  drafts: readonly CenterItem[];
  tasks: readonly CenterItem[];
  approvals: readonly CenterItem[];
  messages: readonly CenterItem[];
  timeline: readonly TimelineEvent[];
  inspector?: CenterItem;
};

export type SourceRecord = IntentReference & {
  kind: CenterItem["kind"];
  title: string;
  summary?: string;
  state: Partial<LifecycleState> & { intent: string };
  owner?: { principalId?: string; teamId?: string };
  updatedAt: string;
  evidence?: unknown;
  restrictedEvidence?: boolean;
  /** Owner API's current actions. The center only filters/annotates them. */
  actions?: readonly Omit<SafeAction, "enabled" | "reason" | "refs">[];
};

export type TimelineSource = Omit<TimelineEvent, "evidence"> & {
  evidence?: unknown;
  restrictedEvidence?: boolean;
};

export type IntentCenterInput = {
  now?: string;
  authority: Authority;
  records?: readonly SourceRecord[];
  timeline?: readonly TimelineSource[];
  inspectIntentId?: string;
};

const terminal = new Set([
  "completed",
  "cancelled",
  "canceled",
  "rejected",
  "failed",
  "closed",
]);
const uncertain = new Set(["ambiguous", "unknown", "inconsistent"]);
const defaultState = (state: SourceRecord["state"]): LifecycleState => ({
  intent: state.intent,
  work: state.work ?? "not_started",
  approval: state.approval ?? "not_required",
  delivery: state.delivery ?? "not_started",
  external: state.external ?? "not_applicable",
});

function redact(
  value: unknown,
  authority: Authority,
  restricted: boolean,
): unknown | RedactedValue {
  if (!restricted) return value;
  if (!authority.canView || !authority.canViewRestrictedEvidence) {
    return {
      redacted: true,
      reason: authority.canView ? "restricted" : "not_authorized",
    };
  }
  return value;
}

function isHidden(
  record: Pick<SourceRecord, "restrictedEvidence">,
  authority: Authority,
): boolean {
  return (
    !authority.canView &&
    record.restrictedEvidence === true &&
    authority.canViewRestrictedEvidence !== true
  );
}

function link(refs: IntentReference, authority: Authority): DeepLink | undefined {
  if (!authority.canView || !refs.intentId) return undefined;
  const params = new URLSearchParams({
    intent: refs.intentId,
    auth: authority.revision,
  });
  if (refs.relationshipId) params.set("relationship", refs.relationshipId);
  if (refs.proposalId) params.set("proposal", refs.proposalId);
  if (refs.workItemId) params.set("workItem", refs.workItemId);
  if (refs.messageId) params.set("message", refs.messageId);
  if (refs.approvalId) params.set("approval", refs.approvalId);
  return {
    href: `/intent-center/${encodeURIComponent(refs.intentId)}?${params}`,
    intentId: refs.intentId,
    refs,
    authorityRevision: authority.revision,
    requiresReauthorization: false,
  };
}

function projectRecord(record: SourceRecord, authority: Authority): CenterItem {
  const state = defaultState(record.state);
  const currentOwner = record.owner ?? {};
  const refs: IntentReference = {
    intentId: record.intentId,
    ...(record.relationshipId ? { relationshipId: record.relationshipId } : {}),
    ...(record.proposalId ? { proposalId: record.proposalId } : {}),
    ...(record.workItemId ? { workItemId: record.workItemId } : {}),
    ...(record.messageId ? { messageId: record.messageId } : {}),
    ...(record.workflowId ? { workflowId: record.workflowId } : {}),
    ...(record.approvalId ? { approvalId: record.approvalId } : {}),
  };
  const stale = !authority.canView;
  const actions = (record.actions ?? []).map((action) => {
    const capable = authority.capabilities.includes(action.capability);
    const terminalBlocked = terminal.has(state.intent) || terminal.has(state.work);
    const ambiguousRetry =
      action.capability === "intent.retry" &&
      (uncertain.has(state.intent) || uncertain.has(state.external));
    const allowed = authority.canView && capable && !terminalBlocked && !ambiguousRetry;
    const reason: SafeAction["reason"] = !authority.canView
      ? "unauthorized"
      : !capable
        ? "unauthorized"
        : terminalBlocked
          ? "terminal"
          : ambiguousRetry
            ? "stale"
            : "unauthorized";
    return { ...action, refs, enabled: allowed, ...(allowed ? {} : { reason }) };
  });
  const item: CenterItem = {
    ...refs,
    refs,
    kind: record.kind,
    title: record.title,
    ...(record.summary === undefined ? {} : { summary: record.summary }),
    state,
    stateLabel: `${state.intent}/${state.work}/${state.approval}/${state.delivery}/${state.external}`,
    stale,
    ...(record.restrictedEvidence === undefined
      ? {}
      : { restricted: record.restrictedEvidence }),
    evidence: redact(record.evidence, authority, !!record.restrictedEvidence),
    actions,
    owner: currentOwner,
    updatedAt: record.updatedAt,
  };
  const deepLink = link(refs, authority);
  if (deepLink) item.deepLink = deepLink;
  return item;
}

/** Build a deterministic, non-authoritative Intent Center read model. */
export function projectIntentCenter(input: IntentCenterInput): IntentCenterProjection {
  const authority = input.authority;
  const records = (input.records ?? [])
    .filter((record) => !isHidden(record, authority))
    .map((record) => projectRecord(record, authority));
  const timeline = (input.timeline ?? [])
    .filter((event) => !isHidden(event, authority))
    .map((event) => ({
      ...event,
      evidence: redact(event.evidence, authority, !!event.restrictedEvidence),
    }));
  const inspector = input.inspectIntentId
    ? records.find((record) => record.intentId === input.inspectIntentId)
    : undefined;
  const projection: IntentCenterProjection = {
    authorityRevision: authority.revision,
    generatedAt: input.now ?? new Date().toISOString(),
    drafts: records.filter((record) => record.kind === "draft"),
    tasks: records.filter((record) => record.kind === "task"),
    approvals: records.filter((record) => record.kind === "approval"),
    messages: records.filter((record) => record.kind === "message"),
    timeline,
  };
  if (inspector) projection.inspector = inspector;
  return projection;
}

export const buildIntentCenterProjection = projectIntentCenter;
export const createIntentCenterProjection = projectIntentCenter;

/** Deep links are usable only when their authority revision is still current. */
export function resolveDeepLink(
  linkValue: DeepLink,
  authority: Authority,
): DeepLink | undefined {
  if (!authority.canView || authority.revision !== linkValue.authorityRevision)
    return undefined;
  return { ...linkValue, requiresReauthorization: false };
}

export function isSafeAction(action: SafeAction, authority: Authority): boolean {
  return (
    action.enabled &&
    authority.canView &&
    authority.capabilities.includes(action.capability) &&
    action.refs.intentId.length > 0
  );
}
