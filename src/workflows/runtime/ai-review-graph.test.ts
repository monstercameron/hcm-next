import { describe, expect, it } from "vitest";
import { WORKFLOW_ROUTE_KEYS } from "@human-capital-management-suite/foundation";
import type { WorkflowConfig } from "../shared/workflow-config.js";
import { advanceWorkflowGraph } from "./graph-runtime.js";

describe("ai_review graph nodes", () => {
  it("auto-advances through ai_review nodes and stops at the next approval", () => {
    const workflowConfig = terminationWorkflowConfigFixture();
    const advance = advanceWorkflowGraph({
      workflowConfig,
      workflowContext: { activeNodeId: "collect_input" },
      firstRouteKey: WORKFLOW_ROUTE_KEYS.SUBMITTED,
      automaticRouteKeysByNodeId: {
        termination_preflight: WORKFLOW_ROUTE_KEYS.VALID,
        ai_review_termination: "completed",
      },
    });

    expect(advance.finalNodeId).toBe("hr_termination_approval");
    expect(advance.completedNodes.map((n) => n.nodeId)).toEqual([
      "collect_input",
      "termination_preflight",
      "ai_review_termination",
    ]);
    expect(advance.nextState).toBe("waiting_approval");
    expect(advance.nextStatus).toBe("waiting");
    expect(advance.nextInteraction).toBe("waitingApproval");
  });

  it("reports ai_review node in completedNodes with 'completed' outcome", () => {
    const workflowConfig = terminationWorkflowConfigFixture();
    const advance = advanceWorkflowGraph({
      workflowConfig,
      workflowContext: { activeNodeId: "collect_input" },
      firstRouteKey: WORKFLOW_ROUTE_KEYS.SUBMITTED,
      automaticRouteKeysByNodeId: {
        termination_preflight: WORKFLOW_ROUTE_KEYS.VALID,
        ai_review_termination: "completed",
      },
    });

    const aiReviewNode = advance.completedNodes.find(
      (n) => n.nodeId === "ai_review_termination",
    );
    expect(aiReviewNode).toBeDefined();
    expect(aiReviewNode?.nodeType).toBe("ai_review");
    expect(aiReviewNode?.outcome).toBe("completed");
    expect(aiReviewNode?.routeKey).toBe("completed");
  });

  it("auto-advances through ai_review via outcomes[0] even without explicit route key mapping", () => {
    const workflowConfig = terminationWorkflowConfigFixture();
    const advance = advanceWorkflowGraph({
      workflowConfig,
      workflowContext: { activeNodeId: "collect_input" },
      firstRouteKey: WORKFLOW_ROUTE_KEYS.SUBMITTED,
      automaticRouteKeysByNodeId: {
        termination_preflight: WORKFLOW_ROUTE_KEYS.VALID,
        // ai_review_termination omitted — graph still advances via outcomes[0]
      },
    });

    expect(advance.completedNodes.map((n) => n.nodeId)).toEqual([
      "collect_input",
      "termination_preflight",
      "ai_review_termination",
    ]);
    expect(advance.finalNodeId).toBe("hr_termination_approval");
    expect(advance.nextState).toBe("waiting_approval");
  });

  it("does not include ai_review type in the stop boundary set", () => {
    const workflowConfig = terminationWorkflowConfigFixture();
    const advance = advanceWorkflowGraph({
      workflowConfig,
      workflowContext: { activeNodeId: "ai_review_termination" },
      firstRouteKey: "completed",
    });

    expect(advance.finalNodeId).toBe("hr_termination_approval");
    expect(advance.completedNodes.map((n) => n.nodeId)).toEqual([
      "ai_review_termination",
    ]);
  });
});

function terminationWorkflowConfigFixture(): WorkflowConfig {
  return {
    intent: "employee.termination",
    subjectType: "worker",
    selfServiceStart: false,
    interactions: {
      input: { type: "form", title: "Initiate termination" },
      waitingApproval: { type: "waiting", title: "Waiting for approval" },
      readyToExecute: { type: "ready_to_execute", title: "Ready to execute" },
    },
    states: {
      collecting_input: {
        actions: [
          {
            transition: "submit_input",
            label: "Submit",
            actor: "hr_admin",
            handler: "submit_configured_input",
            nextState: "waiting_approval",
            nextStatus: "waiting",
          },
        ],
      },
      waiting_approval: {
        actions: [
          {
            transition: "approve",
            label: "Approve",
            actor: "hr_admin",
            handler: "approve",
          },
        ],
      },
    },
    submit: {
      preflightBlock: {
        name: "system.employee_data.termination.preflight",
        version: "1.0.0",
      },
      preflightInput: {},
      effectiveAt: { $source: "input", path: "effectiveAt" },
      businessReason: { $source: "input", path: "businessReason" },
      changeRequestStatus: "in_approval",
      currentSnapshot: {},
      proposedSnapshot: {},
      proposedChange: {
        targetObjectType: "worker",
        targetObjectId: { $source: "workflow", path: "subjectId" },
        currentValue: { $source: "employee", path: "employment.status" },
        proposedValue: { $source: "input", path: "terminationType" },
      },
      additionalEvents: [],
    },
    approval: {
      assigneeActorId: "actor_hr_admin",
      assigneeRole: "hr_admin",
      approvalType: "hr_termination_review",
    },
    plan: {
      block: {
        name: "system.employee_data.termination.plan_transaction",
        version: "1.0.0",
      },
      input: {},
    },
    projection: { allowedPatchPaths: ["/employment/status"] },
    timeline: { businessEvents: [], summaries: {} },
    graph: {
      startNodeId: "collect_input",
      nodes: [
        {
          nodeId: "collect_input",
          type: "interaction",
          title: "Collect termination input",
          state: "collecting_input",
          interaction: "input",
          outcomes: [
            {
              outcome: "submitted",
              routeKey: "submitted",
              nextNodeId: "termination_preflight",
              nextState: "waiting_approval",
              nextStatus: "waiting",
            },
          ],
        },
        {
          nodeId: "termination_preflight",
          type: "block",
          title: "Termination preflight",
          block: {
            name: "system.employee_data.termination.preflight",
            version: "1.0.0",
          },
          outcomes: [
            {
              outcome: "valid",
              routeKey: "valid",
              nextNodeId: "ai_review_termination",
            },
          ],
        },
        {
          nodeId: "ai_review_termination",
          type: "ai_review",
          title: "AI risk assessment",
          aiReview: {
            changeType: "employee.termination",
            failurePolicy: "continue",
            visibleFields: ["terminationType", "effectiveAt"],
            currentStateTemplate: {},
            proposedStateTemplate: {},
          },
          outcomes: [
            {
              outcome: "completed",
              routeKey: "completed",
              nextNodeId: "hr_termination_approval",
              nextState: "waiting_approval",
              nextStatus: "waiting",
              nextInteraction: "waitingApproval",
            },
          ],
        },
        {
          nodeId: "hr_termination_approval",
          type: "approval",
          title: "HR termination approval",
          state: "waiting_approval",
          interaction: "waitingApproval",
          outcomes: [
            {
              outcome: "approved",
              routeKey: "approved",
              nextNodeId: "completed",
              nextState: "approved",
            },
          ],
        },
        {
          nodeId: "completed",
          type: "terminal",
          title: "Completed",
        },
      ],
    },
  };
}
