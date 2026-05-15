import { describe, expect, it } from "vitest";
import { ERROR_CODES } from "@hcm-next/foundation";
import {
  evaluateApprovalGate,
  type ApprovalGateDecision,
  type ApprovalGateEvaluationInput,
  type ApprovalGateTaskEvaluationInput,
} from "./approval-gate.js";

describe("approval gate evaluator", () => {
  it("opens only the first sequential task when the gate starts", () => {
    const decision = evaluate({
      mode: "sequential",
      passRule: { type: "all_required" },
      tasks: [],
      totalTaskCount: 2,
    });

    expect(decision.outcome).toBe("waiting");
    expect(decision.openSequenceIndexes).toEqual([0]);
    expect(decision.currentSequenceIndex).toBe(0);
  });

  it("advances a sequential gate to the next task on approval", () => {
    const decision = evaluate({
      mode: "sequential",
      passRule: { type: "all_required" },
      tasks: [task("task_1", "approved", { sequenceIndex: 0 })],
      totalTaskCount: 2,
    });

    expect(decision.outcome).toBe("advance_sequence");
    expect(decision.openSequenceIndexes).toEqual([1]);
  });

  it("passes a sequential gate after the final ordered approval", () => {
    const decision = evaluate({
      mode: "sequential",
      passRule: { type: "all_required" },
      tasks: [
        task("task_1", "approved", { sequenceIndex: 0 }),
        task("task_2", "approved", { sequenceIndex: 1 }),
      ],
      totalTaskCount: 2,
    });

    expect(decision.outcome).toBe("passed");
  });

  it("fails a sequential gate on rejection when policy stops the workflow", () => {
    const decision = evaluate({
      mode: "sequential",
      passRule: { type: "all_required" },
      failurePolicy: "stop_workflow",
      tasks: [task("task_1", "rejected", { sequenceIndex: 0 })],
      totalTaskCount: 2,
    });

    expect(decision.outcome).toBe("failed");
  });

  it("routes a sequential gate to repair when the failure policy says so", () => {
    const decision = evaluate({
      mode: "sequential",
      passRule: { type: "all_required" },
      failurePolicy: "send_to_repair",
      tasks: [task("task_1", "rejected", { sequenceIndex: 0 })],
      totalTaskCount: 2,
    });

    expect(decision.outcome).toBe("repair");
  });

  it("passes a parallel 3 of 5 gate on the third approval", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "quorum", requiredApprovals: 3 },
      tasks: [
        task("task_1", "approved"),
        task("task_2", "approved"),
        task("task_3", "approved"),
        task("task_4", "pending"),
        task("task_5", "pending"),
      ],
    });

    expect(decision.outcome).toBe("passed");
    expect(decision.cancelPendingTaskIds).toEqual(["task_4", "task_5"]);
  });

  it("does not pass a parallel 3 of 5 gate after only two approvals", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "quorum", requiredApprovals: 3 },
      tasks: [
        task("task_1", "approved"),
        task("task_2", "approved"),
        task("task_3", "pending"),
        task("task_4", "pending"),
        task("task_5", "pending"),
      ],
    });

    expect(decision.outcome).toBe("waiting");
  });

  it("fails a parallel 3 of 5 gate after three rejections", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "quorum", requiredApprovals: 3 },
      tasks: [
        task("task_1", "rejected"),
        task("task_2", "rejected"),
        task("task_3", "rejected"),
        task("task_4", "pending"),
        task("task_5", "pending"),
      ],
    });

    expect(decision.outcome).toBe("failed");
    expect(decision.reason).toBe("quorum_impossible");
  });

  it("waits when a parallel 3 of 5 gate is still mathematically possible", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "quorum", requiredApprovals: 3 },
      tasks: [
        task("task_1", "approved"),
        task("task_2", "approved"),
        task("task_3", "rejected"),
        task("task_4", "rejected"),
        task("task_5", "pending"),
      ],
    });

    expect(decision.outcome).toBe("waiting");
    expect(decision.reason).toBe("quorum_still_possible");
  });

  it("fails immediately when a veto holder rejects", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "quorum", requiredApprovals: 3 },
      tasks: [
        task("task_1", "rejected", { isVetoHolder: true }),
        task("task_2", "pending"),
        task("task_3", "pending"),
        task("task_4", "pending"),
        task("task_5", "pending"),
      ],
    });

    expect(decision.outcome).toBe("failed");
    expect(decision.reason).toBe("veto_rejected");
  });

  it("passes a weighted rule when approval weight reaches the threshold", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "weighted", requiredWeight: 6 },
      tasks: [
        task("task_1", "approved", { weight: 4 }),
        task("task_2", "approved", { weight: 2 }),
        task("task_3", "pending", { weight: 1 }),
      ],
    });

    expect(decision.outcome).toBe("passed");
    expect(decision.progress.approvalWeight).toBe(6);
  });

  it("passes a percentage rule at the configured percentage", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "percentage", requiredPercentage: 0.6 },
      tasks: [
        task("task_1", "approved"),
        task("task_2", "approved"),
        task("task_3", "approved"),
        task("task_4", "pending"),
        task("task_5", "pending"),
      ],
    });

    expect(decision.outcome).toBe("passed");
    expect(decision.progress.requiredApprovals).toBe(3);
  });

  it("passes an any-one rule on the first approval", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "any_one" },
      tasks: [task("task_1", "approved"), task("task_2", "pending")],
    });

    expect(decision.outcome).toBe("passed");
  });

  it("requires each configured role bucket for role quorum", () => {
    const waitingDecision = evaluate({
      mode: "parallel",
      passRule: {
        type: "role_quorum",
        roleQuorums: [
          { role: "finance", requiredApprovals: 1 },
          { role: "medical_director", requiredApprovals: 1 },
        ],
      },
      tasks: [
        task("task_1", "approved", { assigneeRole: "finance" }),
        task("task_2", "pending", { assigneeRole: "medical_director" }),
      ],
    });
    const passedDecision = evaluate({
      mode: "parallel",
      passRule: {
        type: "role_quorum",
        roleQuorums: [
          { role: "finance", requiredApprovals: 1 },
          { role: "medical_director", requiredApprovals: 1 },
        ],
      },
      tasks: [
        task("task_1", "approved", { assigneeRole: "finance" }),
        task("task_2", "approved", { assigneeRole: "medical_director" }),
      ],
    });

    expect(waitingDecision.outcome).toBe("waiting");
    expect(passedDecision.outcome).toBe("passed");
  });

  it("requires every child rule in a composite rule", () => {
    const waitingDecision = evaluate({
      mode: "parallel",
      passRule: {
        type: "composite",
        rules: [
          { type: "quorum", requiredApprovals: 2 },
          { type: "weighted", requiredWeight: 5 },
        ],
      },
      tasks: [
        task("task_1", "approved", { weight: 3 }),
        task("task_2", "approved", { weight: 1 }),
        task("task_3", "pending", { weight: 1 }),
      ],
    });
    const passedDecision = evaluate({
      mode: "parallel",
      passRule: {
        type: "composite",
        rules: [
          { type: "quorum", requiredApprovals: 2 },
          { type: "weighted", requiredWeight: 5 },
        ],
      },
      tasks: [
        task("task_1", "approved", { weight: 3 }),
        task("task_2", "approved", { weight: 2 }),
      ],
    });

    expect(waitingDecision.outcome).toBe("waiting");
    expect(passedDecision.outcome).toBe("passed");
  });

  it("ignores pending tasks for final counts except impossibility math", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "quorum", requiredApprovals: 3 },
      tasks: [
        task("task_1", "approved"),
        task("task_2", "approved"),
        task("task_3", "pending"),
        task("task_4", "pending"),
      ],
    });

    expect(decision.outcome).toBe("waiting");
    expect(decision.progress.approvedCount).toBe(2);
    expect(decision.progress.pendingCount).toBe(2);
  });

  it("does not count canceled or superseded tasks", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "quorum", requiredApprovals: 2 },
      tasks: [
        task("task_1", "approved"),
        task("task_2", "canceled", { decision: "approved" }),
        task("task_3", "superseded"),
      ],
    });

    expect(decision.outcome).toBe("failed");
    expect(decision.progress.approvedCount).toBe(1);
    expect(decision.progress.ignoredCount).toBe(2);
  });

  it("does not double-count duplicate task decisions", () => {
    const decision = evaluate({
      mode: "parallel",
      passRule: { type: "quorum", requiredApprovals: 2 },
      tasks: [
        task("task_1", "approved"),
        task("task_1", "approved"),
        task("task_2", "pending"),
      ],
    });

    expect(decision.outcome).toBe("waiting");
    expect(decision.progress.approvedCount).toBe(1);
  });

  it("returns a version conflict for stale task decisions", () => {
    const result = evaluateApprovalGate({
      mode: "parallel",
      passRule: { type: "any_one" },
      tasks: [
        task("task_1", "approved", {
          taskVersion: 1,
          expectedTaskVersion: 2,
        }),
      ],
    });

    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("Expected task version conflict.");
    }
    expect(result.error.code).toBe(ERROR_CODES.VERSION_CONFLICT);
  });
});

function evaluate(input: ApprovalGateEvaluationInput): ApprovalGateDecision {
  const result = evaluateApprovalGate(input);

  expect(result.ok).toBe(true);
  if (!result.ok) {
    throw new Error(result.error.message);
  }

  return result.value;
}

function task(
  approvalTaskId: string,
  status: ApprovalGateTaskEvaluationInput["status"],
  overrides: Partial<ApprovalGateTaskEvaluationInput> = {},
): ApprovalGateTaskEvaluationInput {
  return {
    approvalTaskId,
    status,
    ...overrides,
  };
}
