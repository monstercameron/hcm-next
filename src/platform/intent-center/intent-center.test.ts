import { describe, expect, it } from "vitest";
import {
  isSafeAction,
  projectIntentCenter,
  resolveDeepLink,
  type IntentCenterInput,
} from "./index";

const base = (): IntentCenterInput => ({
  now: "2026-09-03T12:00:00.000Z",
  authority: {
    principalId: "actor-1",
    tenantId: "tenant-1",
    revision: "auth-7",
    canView: true,
    capabilities: ["intent.inspect", "approval.decide"],
    canViewRestrictedEvidence: false,
  },
  records: [
    {
      kind: "approval",
      intentId: "intent-1",
      relationshipId: "rel-1",
      proposalId: "proposal-1",
      workItemId: "work-1",
      approvalId: "approval-1",
      title: "Approve legal name change",
      state: {
        intent: "in_progress",
        work: "waiting",
        approval: "pending",
        delivery: "not_started",
        external: "not_applicable",
      },
      owner: { principalId: "actor-1" },
      updatedAt: "2026-09-03T11:00:00.000Z",
      actions: [{ id: "approve", capability: "approval.decide", label: "Approve" }],
      evidence: { proposal: "proposal-1" },
    },
  ],
  timeline: [
    {
      eventId: "event-1",
      intentId: "intent-1",
      approvalId: "approval-1",
      type: "approval.created",
      occurredAt: "2026-09-03T10:00:00.000Z",
      summary: "Approval requested",
      evidence: { secret: "hidden" },
      restrictedEvidence: true,
    },
  ],
});

describe("UX-007 Intent Center contract", () => {
  it("TestIntentCenterPreservesAuthorityAndLifecycleTruth", () => {
    const projection = projectIntentCenter(base());
    const approval = projection.approvals[0]!;
    expect(approval.refs).toEqual({
      intentId: "intent-1",
      relationshipId: "rel-1",
      proposalId: "proposal-1",
      workItemId: "work-1",
      approvalId: "approval-1",
    });
    expect(approval.state.approval).toBe("pending");
    expect(approval.stateLabel).toBe(
      "in_progress/waiting/pending/not_started/not_applicable",
    );
    expect(approval.actions[0]!.enabled).toBe(true);
  });

  it("TestTodo_UX_007_Integration", () => {
    const projection = projectIntentCenter(base());
    expect(projection.approvals[0]!.intentId).toBe(projection.timeline[0]!.intentId);
    expect(projection.approvals[0]!.approvalId).toBe(
      projection.timeline[0]!.approvalId,
    );
    expect(projection.generatedAt).toBe("2026-09-03T12:00:00.000Z");
  });

  it("TestTodo_UX_007_Fault", () => {
    const input = base();
    input.authority = { ...input.authority, canView: false, capabilities: [] };
    const item = projectIntentCenter(input).approvals[0]!;
    expect(item.actions[0]).toMatchObject({ enabled: false, reason: "unauthorized" });
    expect(item.deepLink).toBeUndefined();
  });

  it("TestTodo_UX_007_Security", () => {
    const projection = projectIntentCenter(base());
    expect(projection.timeline[0]!.evidence).toEqual({
      redacted: true,
      reason: "restricted",
    });
  });

  it("TestTodo_UX_007_Browser", () => {
    const item = projectIntentCenter(base()).approvals[0]!;
    expect(item.deepLink?.href).toContain("/intent-center/intent-1?");
    expect(item.deepLink?.href).toContain("approval=approval-1");
    expect(resolveDeepLink(item.deepLink!, base().authority)).toBeDefined();
  });

  it("TestTodo_UX_007_Mutation", () => {
    const projection = projectIntentCenter(base());
    const action = projection.approvals[0]!.actions[0]!;
    expect(isSafeAction(action, base().authority)).toBe(true);
    expect(isSafeAction({ ...action, refs: { intentId: "" } }, base().authority)).toBe(
      false,
    );
    expect(projectIntentCenter(base()).approvals[0]!.state.approval).toBe("pending");
  });
});
