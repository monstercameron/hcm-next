import { describe, expect, it } from "vitest";
import { WORKFLOW_ROUTE_KEYS } from "@human-capital-management-suite/foundation";
import type { WorkflowConfig } from "../shared/workflow-config.js";
import {
  advanceWorkflowGraph,
  contextWithGraphAdvance,
  routeDecisionForNode,
} from "./graph-runtime.js";

describe("generic graph runtime", () => {
  it("lets graph edges override node outcome routing", () => {
    const workflowConfig = workflowConfigFixture();
    const startNode = workflowConfig.graph?.nodes[0];

    expect(startNode).toBeDefined();
    if (startNode === undefined) {
      return;
    }

    const decision = routeDecisionForNode({
      workflowConfig,
      node: startNode,
      routeKey: WORKFLOW_ROUTE_KEYS.SUBMITTED,
    });

    expect(decision).toMatchObject({
      fromNodeId: "collect_input",
      routeKey: WORKFLOW_ROUTE_KEYS.SUBMITTED,
      toNodeId: "preflight",
      nextState: "waiting_approval",
      nextStatus: "waiting",
      eventType: "EdgeSubmitted",
    });
  });

  it("auto-advances through automatic nodes and stops at wait boundaries", () => {
    const workflowConfig = workflowConfigFixture();
    const advance = advanceWorkflowGraph({
      workflowConfig,
      workflowContext: { activeNodeId: "collect_input" },
      firstRouteKey: WORKFLOW_ROUTE_KEYS.SUBMITTED,
      automaticRouteKeysByNodeId: {
        preflight: WORKFLOW_ROUTE_KEYS.VALID,
      },
    });

    expect(advance.finalNodeId).toBe("approval");
    expect(advance.nextState).toBe("waiting_approval");
    expect(advance.nextStatus).toBe("waiting");
    expect(advance.completedNodes.map((node) => node.nodeId)).toEqual([
      "collect_input",
      "preflight",
    ]);

    const context = contextWithGraphAdvance({
      context: { activeNodeId: "collect_input" },
      advance,
    });

    expect(context["activeNodeId"]).toBe("approval");
    expect(
      (context["graphRuntime"] as Record<string, unknown>)["completedNodes"],
    ).toHaveLength(2);
  });

  it("stops before side-effect nodes when requested", () => {
    const workflowConfig = workflowConfigFixture();
    const advance = advanceWorkflowGraph({
      workflowConfig,
      workflowContext: { activeNodeId: "approval" },
      firstRouteKey: WORKFLOW_ROUTE_KEYS.APPROVED,
      automaticRouteKeysByNodeId: {
        plan_transaction: WORKFLOW_ROUTE_KEYS.PLANNED,
      },
      stopBeforeNodeTypes: ["projection_write"],
    });

    expect(advance.finalNodeId).toBe("apply_projection");
    expect(advance.completedNodes.map((node) => node.nodeId)).toEqual([
      "approval",
      "plan_transaction",
    ]);
  });

  it("protects against graph loops with a max visit warning", () => {
    const workflowConfig = loopWorkflowConfigFixture();
    const advance = advanceWorkflowGraph({
      workflowConfig,
      workflowContext: { activeNodeId: "loop_a" },
      automaticRouteKeysByNodeId: {
        loop_a: "again",
        loop_b: "again",
      },
      maxNodeVisits: 3,
    });

    expect(advance.completedNodes).toHaveLength(3);
    expect(advance.warnings).toContain(
      "Graph advancement stopped after max node visits.",
    );
  });
});

function workflowConfigFixture(): WorkflowConfig {
  return {
    intent: "test.workflow",
    subjectType: "worker",
    selfServiceStart: true,
    interactions: {},
    states: {},
    submit: {} as WorkflowConfig["submit"],
    approval: {} as WorkflowConfig["approval"],
    plan: {} as WorkflowConfig["plan"],
    projection: { allowedPatchPaths: [] },
    timeline: { businessEvents: [], summaries: {} },
    graph: {
      startNodeId: "collect_input",
      nodes: [
        {
          nodeId: "collect_input",
          type: "interaction",
          title: "Collect input",
          outcomes: [
            {
              outcome: WORKFLOW_ROUTE_KEYS.SUBMITTED,
              routeKey: WORKFLOW_ROUTE_KEYS.SUBMITTED,
              eventType: "NodeSubmitted",
              nextNodeId: "wrong_node",
            },
          ],
        },
        {
          nodeId: "preflight",
          type: "block",
          title: "Preflight",
          outcomes: [
            {
              outcome: WORKFLOW_ROUTE_KEYS.VALID,
              routeKey: WORKFLOW_ROUTE_KEYS.VALID,
              nextNodeId: "approval",
            },
          ],
        },
        {
          nodeId: "approval",
          type: "approval",
          title: "Approval",
          outcomes: [
            {
              outcome: WORKFLOW_ROUTE_KEYS.APPROVED,
              routeKey: WORKFLOW_ROUTE_KEYS.APPROVED,
              nextNodeId: "plan_transaction",
              nextState: "approved",
              nextStatus: "active",
            },
          ],
        },
        {
          nodeId: "plan_transaction",
          type: "transaction_plan",
          title: "Plan transaction",
          outcomes: [
            {
              outcome: WORKFLOW_ROUTE_KEYS.PLANNED,
              routeKey: WORKFLOW_ROUTE_KEYS.PLANNED,
              nextNodeId: "apply_projection",
            },
          ],
        },
        {
          nodeId: "apply_projection",
          type: "projection_write",
          title: "Apply projection",
          outcomes: [
            {
              outcome: WORKFLOW_ROUTE_KEYS.APPLIED,
              routeKey: WORKFLOW_ROUTE_KEYS.APPLIED,
              nextNodeId: "completed",
            },
          ],
        },
        {
          nodeId: "completed",
          type: "terminal",
          title: "Completed",
        },
      ],
      edges: [
        {
          fromNodeId: "collect_input",
          routeKey: WORKFLOW_ROUTE_KEYS.SUBMITTED,
          toNodeId: "preflight",
          eventType: "EdgeSubmitted",
          nextState: "waiting_approval",
          nextStatus: "waiting",
        },
      ],
    },
  } as WorkflowConfig;
}

function loopWorkflowConfigFixture(): WorkflowConfig {
  return {
    ...workflowConfigFixture(),
    graph: {
      startNodeId: "loop_a",
      nodes: [
        {
          nodeId: "loop_a",
          type: "block",
          title: "Loop A",
          outcomes: [
            {
              outcome: "again",
              routeKey: "again",
              nextNodeId: "loop_b",
            },
          ],
        },
        {
          nodeId: "loop_b",
          type: "block",
          title: "Loop B",
          outcomes: [
            {
              outcome: "again",
              routeKey: "again",
              nextNodeId: "loop_a",
            },
          ],
        },
      ],
    },
  } as WorkflowConfig;
}
