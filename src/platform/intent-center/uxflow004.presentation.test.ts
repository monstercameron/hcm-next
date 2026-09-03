import { describe, expect, it } from "vitest";
import { projectIntentCenter, type Authority, type SourceRecord } from "./index";

const authority = (overrides: Partial<Authority> = {}): Authority => ({
  principalId: "participant-1",
  tenantId: "tenant-1",
  revision: "auth-1",
  canView: true,
  capabilities: ["intent.retry", "intent.refresh", "intent.correct"],
  ...overrides,
});

const source = (
  state: SourceRecord["state"],
  overrides: Partial<SourceRecord> = {},
): SourceRecord => ({
  intentId: "intent-1",
  kind: "task",
  title: "Review request",
  state,
  updatedAt: "2026-09-03T12:00:00.000Z",
  actions: [
    { id: "retry", capability: "intent.retry", label: "Retry" },
    { id: "refresh", capability: "intent.refresh", label: "Refresh" },
    { id: "correct", capability: "intent.correct", label: "Correct" },
  ],
  ...overrides,
});

describe("UXFLOW-004 presentation matrix", () => {
  it.each([
    [
      "success",
      {
        intent: "completed",
        work: "completed",
        approval: "approved",
        delivery: "delivered",
        external: "consistent",
      },
      "completed/completed/approved/delivered/consistent",
    ],
    [
      "partial",
      {
        intent: "submitted",
        work: "completed",
        approval: "approved",
        delivery: "delivered",
        external: "pending",
      },
      "submitted/completed/approved/delivered/pending",
    ],
    [
      "unknown",
      {
        intent: "submitted",
        work: "in_progress",
        approval: "approved",
        delivery: "unknown",
        external: "unknown",
      },
      "submitted/in_progress/approved/unknown/unknown",
    ],
    [
      "repair",
      {
        intent: "submitted",
        work: "repair_required",
        approval: "approved",
        delivery: "blocked",
        external: "inconsistent",
      },
      "submitted/repair_required/approved/blocked/inconsistent",
    ],
  ])("preserves every multidimensional %s state exactly", (_name, state, label) => {
    const item = projectIntentCenter({
      authority: authority(),
      records: [source(state)],
    }).tasks[0]!;
    expect(item.state).toEqual(state);
    expect(item.stateLabel).toBe(label);
  });

  it("disables all actions when the participant loses current authorization", () => {
    const item = projectIntentCenter({
      authority: authority({ canView: false, capabilities: [] }),
      records: [source({ intent: "submitted" })],
    }).tasks[0]!;
    expect(item.stale).toBe(true);
    expect(
      item.actions.every(
        (action) => action.enabled === false && action.reason === "unauthorized",
      ),
    ).toBe(true);
    expect(item.deepLink).toBeUndefined();
  });

  it("does not expose an effect-producing retry for an ambiguous outcome", () => {
    const item = projectIntentCenter({
      authority: authority(),
      records: [source({ intent: "ambiguous", external: "ambiguous" })],
    }).tasks[0]!;
    const retry = item.actions.find((action) => action.id === "retry");
    expect(retry).toMatchObject({ enabled: false });
  });

  it("does not disclose denied records or restricted evidence", () => {
    const projection = projectIntentCenter({
      authority: authority({ canView: false, capabilities: [] }),
      records: [
        source(
          { intent: "completed" },
          { restrictedEvidence: true, evidence: { provider: "secret" } },
        ),
      ],
    });
    expect(projection.tasks).toHaveLength(0);
    expect(projection.inspector).toBeUndefined();
  });
});
