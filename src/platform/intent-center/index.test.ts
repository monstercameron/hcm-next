import { describe, expect, it } from "vitest";
import {
  isSafeAction,
  projectIntentCenter,
  resolveDeepLink,
  type Authority,
  type SafeAction,
  type SourceRecord,
} from "./index";

const authority = (overrides: Partial<Authority> = {}): Authority => ({
  principalId: "principal-1",
  tenantId: "tenant-1",
  revision: "auth-7",
  canView: true,
  capabilities: ["intent.retry", "intent.cancel", "intent.correct"],
  ...overrides,
});

const actions: readonly Omit<SafeAction, "enabled" | "reason" | "refs">[] = [
  { id: "retry", capability: "intent.retry", label: "Retry" },
  { id: "cancel", capability: "intent.cancel", label: "Cancel" },
  { id: "correct", capability: "intent.correct", label: "Correct" },
];

const record = (overrides: Partial<SourceRecord> = {}): SourceRecord => ({
  intentId: "intent-1",
  relationshipId: "relationship-2",
  proposalId: "proposal-3",
  workItemId: "work-4",
  messageId: "message-5",
  workflowId: "workflow-6",
  approvalId: "approval-7",
  kind: "task",
  title: "Review change",
  state: { intent: "submitted" },
  owner: { principalId: "owner-1", teamId: "team-1" },
  updatedAt: "2026-09-03T12:00:00.000Z",
  evidence: { source: "owner-api" },
  actions,
  ...overrides,
});

describe("UX-007 Intent Center projection matrix", () => {
  it.each([
    ["retry", "intent.retry"],
    ["cancel", "intent.cancel"],
    ["correct", "intent.correct"],
  ])("enables the safe %s action only with its exact capability", (id, capability) => {
    const item = projectIntentCenter({
      authority: authority({ capabilities: [capability] }),
      records: [record()],
    }).tasks[0]!;
    const action = item.actions.find((candidate) => candidate.id === id);
    expect(action).toMatchObject({
      id,
      capability,
      enabled: true,
      refs: expect.objectContaining({ intentId: "intent-1" }),
    });
  });

  it("fails closed on authority loss and redacts restricted evidence", () => {
    const denied = authority({
      canView: false,
      capabilities: [],
      canViewRestrictedEvidence: true,
    });
    const item = projectIntentCenter({
      authority: denied,
      records: [record({ restrictedEvidence: true })],
    }).tasks[0]!;

    expect(item.stale).toBe(true);
    expect(item.deepLink).toBeUndefined();
    expect(item.evidence).toEqual({ redacted: true, reason: "not_authorized" });
    expect(
      item.actions.every(
        (action) => !action.enabled && action.reason === "unauthorized",
      ),
    ).toBe(true);
  });

  it("distinguishes restricted evidence from authority denial", () => {
    const item = projectIntentCenter({
      authority: authority(),
      records: [record({ restrictedEvidence: true })],
    }).tasks[0]!;
    expect(item.evidence).toEqual({ redacted: true, reason: "restricted" });

    const permitted = projectIntentCenter({
      authority: authority({ canViewRestrictedEvidence: true }),
      records: [record({ restrictedEvidence: true })],
    }).tasks[0]!;
    expect(permitted.evidence).toEqual({ source: "owner-api" });
  });

  it("disables every action at a terminal lifecycle state", () => {
    const item = projectIntentCenter({
      authority: authority(),
      records: [record({ state: { intent: "completed" } })],
    }).tasks[0]!;
    expect(
      item.actions.every((action) => !action.enabled && action.reason === "terminal"),
    ).toBe(true);
  });

  it("preserves exact owner references on actions, inspector, timeline, and deep links", () => {
    const refs = record();
    const projection = projectIntentCenter({
      authority: authority(),
      inspectIntentId: refs.intentId,
      records: [refs],
      timeline: [
        {
          ...refs,
          eventId: "event-1",
          occurredAt: refs.updatedAt,
          type: "submitted",
          summary: "Submitted",
        },
      ],
    });
    const item = projection.tasks[0]!;
    expect(item).toMatchObject({
      intentId: refs.intentId,
      relationshipId: refs.relationshipId,
      proposalId: refs.proposalId,
      workItemId: refs.workItemId,
      messageId: refs.messageId,
      workflowId: refs.workflowId,
      approvalId: refs.approvalId,
    });
    for (const action of item.actions) {
      expect(action.refs).toMatchObject({
        intentId: refs.intentId,
        approvalId: refs.approvalId,
      });
    }
    expect(projection.inspector).toBe(item);
    expect(projection.timeline[0]).toMatchObject({
      intentId: refs.intentId,
      approvalId: refs.approvalId,
    });
    expect(item.deepLink).toMatchObject({
      intentId: refs.intentId,
      authorityRevision: "auth-7",
      requiresReauthorization: false,
    });
    expect(item.deepLink?.href).toContain("relationship=relationship-2");
    expect(item.deepLink?.href).toContain("approval=approval-7");
  });

  it("rejects stale or unauthorized deep links", () => {
    const item = projectIntentCenter({ authority: authority(), records: [record()] })
      .tasks[0]!;
    expect(item.deepLink).toBeDefined();
    expect(
      resolveDeepLink(item.deepLink!, authority({ revision: "auth-8" })),
    ).toBeUndefined();
    expect(
      resolveDeepLink(item.deepLink!, authority({ canView: false })),
    ).toBeUndefined();
    expect(resolveDeepLink(item.deepLink!, authority())).toMatchObject({
      authorityRevision: "auth-7",
      requiresReauthorization: false,
    });
  });

  it("requires both projected enablement and current authority for safe actions", () => {
    const item = projectIntentCenter({
      authority: authority({ capabilities: ["intent.retry"] }),
      records: [record()],
    }).tasks[0]!;
    const retry = item.actions.find((action) => action.id === "retry")!;
    expect(isSafeAction(retry, authority())).toBe(true);
    expect(isSafeAction(retry, authority({ canView: false }))).toBe(false);
    expect(isSafeAction({ ...retry, enabled: false }, authority())).toBe(false);
    expect(isSafeAction({ ...retry, refs: { intentId: "" } }, authority())).toBe(false);
  });
});
