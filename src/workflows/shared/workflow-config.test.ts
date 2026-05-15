import { describe, expect, it } from "vitest";
import {
  LEDGER_EVENT_TYPES,
  WORKFLOW_INTENTS,
  type WorkflowState,
} from "@hcm-next/foundation";
import {
  getWorkflowConfigByIntent,
  type WorkflowConfig,
  type WorkflowGraphNodeConfig,
} from "./workflow-config.js";

const orgTransferIntent = WORKFLOW_INTENTS.EMPLOYEE_ORG_TRANSFER_COMPENSATION_CHANGE;

const lowerSnakeCasePattern = /^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$/;

const knownActionActors = new Set([
  "requester",
  "initiator",
  "hr_admin",
  "source_manager",
  "destination_manager",
  "finance_admin",
  "compensation_admin",
  "medical_director",
  "clinic_ops_admin",
  "org_transfer_approver",
  "hr_admin_or_system",
]);

const knownActionHandlers = new Set([
  "submit_configured_input",
  "provide_evidence",
  "approve",
  "reject",
  "request_more_info",
  "cancel",
  "execute",
]);

describe("employee org transfer compensation workflow config", () => {
  it("is registered by intent with HR-only start", () => {
    const workflowConfig = loadOrgTransferWorkflowConfig();

    expect(workflowConfig.intent).toBe(orgTransferIntent);
    expect(workflowConfig.subjectType).toBe("worker");
    expect(workflowConfig.selfServiceStart).toBe(false);
    expect(workflowConfig.startActors).toEqual(["hr_admin"]);
  });

  it("has a valid node graph contract", () => {
    const workflowConfig = loadOrgTransferWorkflowConfig();
    const graph = workflowConfig.graph;

    expect(graph).toBeDefined();
    if (graph === undefined) {
      return;
    }

    const nodesById = new Map(
      graph.nodes.map((node) => {
        return [node.nodeId, node];
      }),
    );
    const stateKeys = new Set(Object.keys(workflowConfig.states));
    const interactionKeys = new Set(Object.keys(workflowConfig.interactions));
    const ledgerEventTypes = new Set<string>(Object.values(LEDGER_EVENT_TYPES));
    const startNodes = graph.nodes.filter((node) => {
      return node.nodeId === graph.startNodeId;
    });

    expect(startNodes).toHaveLength(1);

    for (const node of graph.nodes) {
      expect(node.title.trim().length, node.nodeId).toBeGreaterThan(0);

      if (node.state !== undefined) {
        expect(stateKeys.has(node.state), `${node.nodeId} state`).toBe(true);
      }

      if (node.interaction !== undefined) {
        expect(
          interactionKeys.has(node.interaction),
          `${node.nodeId} interaction`,
        ).toBe(true);
      }

      if (node.type !== "terminal") {
        expect(node.outcomes?.length ?? 0, `${node.nodeId} outcomes`).toBeGreaterThan(
          0,
        );
      }

      for (const outcome of node.outcomes ?? []) {
        expect(
          lowerSnakeCasePattern.test(outcome.outcome),
          `${node.nodeId}.${outcome.outcome}`,
        ).toBe(true);

        if (outcome.nextNodeId !== undefined) {
          expect(
            nodesById.has(outcome.nextNodeId),
            `${node.nodeId}.${outcome.outcome} nextNodeId`,
          ).toBe(true);
        }

        if (outcome.nextState !== undefined) {
          expect(
            stateKeys.has(outcome.nextState),
            `${node.nodeId}.${outcome.outcome} nextState`,
          ).toBe(true);
        }

        if (outcome.nextInteraction !== undefined) {
          expect(
            interactionKeys.has(outcome.nextInteraction),
            `${node.nodeId}.${outcome.outcome} nextInteraction`,
          ).toBe(true);
        }

        if (outcome.eventType !== undefined) {
          expect(
            ledgerEventTypes.has(outcome.eventType),
            `${node.nodeId}.${outcome.outcome} eventType`,
          ).toBe(true);
        }
      }
    }
  });

  it("uses known actors, handlers, and states for configured actions", () => {
    const workflowConfig = loadOrgTransferWorkflowConfig();
    const stateKeys = new Set(Object.keys(workflowConfig.states));

    for (const [state, stateConfig] of Object.entries(workflowConfig.states)) {
      expect(stateKeys.has(state), `${state} exists`).toBe(true);

      for (const action of stateConfig.actions) {
        expect(knownActionActors.has(action.actor), `${state} actor`).toBe(true);
        expect(knownActionHandlers.has(action.handler), `${state} handler`).toBe(true);
        expect(action.transition.trim().length, `${state} transition`).toBeGreaterThan(
          0,
        );

        if (action.nextState !== undefined) {
          expect(
            stateKeys.has(action.nextState as WorkflowState),
            `${state} nextState`,
          ).toBe(true);
        }
      }
    }
  });

  it("defines the expected serial approval and external write outcomes", () => {
    const workflowConfig = loadOrgTransferWorkflowConfig();
    const graph = requireGraph(workflowConfig);
    const approvalNodes = graph.nodes.filter((node) => {
      return node.type === "approval";
    });
    const externalWriteNodes = graph.nodes.filter((node) => {
      return node.type === "external_write";
    });

    expect(nodeIds(approvalNodes)).toEqual([
      "source_manager_approval",
      "destination_manager_approval",
      "finance_approval",
      "compensation_approval",
      "medical_director_approval",
    ]);

    for (const approvalNode of approvalNodes) {
      expect(outcomeKeys(approvalNode), approvalNode.nodeId).toEqual(
        expect.arrayContaining(["approved", "rejected", "request_more_info"]),
      );
    }

    expect(nodeIds(externalWriteNodes)).toEqual([
      "sync_hris_transfer",
      "sync_payroll_cost_center",
      "sync_compensation_vendor",
    ]);

    for (const externalWriteNode of externalWriteNodes) {
      expect(outcomeKeys(externalWriteNode), externalWriteNode.nodeId).toEqual(
        expect.arrayContaining([
          "accepted",
          "rejected",
          "retryable_failure",
          "dead_letter",
        ]),
      );
    }
  });

  it("includes the input schema and UX-contract metadata needed by generated UI", () => {
    const workflowConfig = loadOrgTransferWorkflowConfig();
    const inputInteraction = requireRecord(workflowConfig.interactions["input"]);
    const jsonSchema = requireRecord(inputInteraction["jsonSchema"]);
    const uiSchema = requireRecord(inputInteraction["uiSchema"]);
    const requiredFields = requireStringArray(jsonSchema["required"]);
    const employeeContext = requireArray(inputInteraction["employeeContext"]);
    const uxContract = requireRecord(
      (workflowConfig as unknown as Record<string, unknown>)["uxContract"],
    );

    expect(requiredFields).toEqual(
      expect.arrayContaining([
        "targetLocationOrgUnitId",
        "targetTeamOrgUnitId",
        "targetCostCenterOrgUnitId",
        "targetManagerEmployeeId",
        "proposedJob",
        "proposedCompensation",
        "effectiveAt",
        "businessReason",
        "transferReason",
        "accessImpactAcknowledged",
      ]),
    );
    expect(contextOutputKeys(employeeContext)).toEqual(
      expect.arrayContaining([
        "currentOrganization",
        "currentJob",
        "currentManager",
        "currentCompensation",
        "activeWorkerAssignments",
        "currentRoleBindingSummary",
      ]),
    );
    expect(uiSchema["submitLabel"]).toBe("Submit transfer for review");
    expect(uiSchema["warningTextKeys"]).toEqual(
      expect.arrayContaining(["org_transfer.access_added_for_destination_manager"]),
    );
    expect(uxContract["restrictedSummaryPermission"]).toBe(
      "employee_data_change.org_transfer.view_restricted_summary",
    );

    for (const interactionKey of [
      "sourceManagerApproval",
      "destinationManagerApproval",
      "financeApproval",
      "compensationApproval",
      "medicalDirectorApproval",
      "readyToExecute",
      "repair",
      "completedSummary",
    ]) {
      expect(workflowConfig.interactions[interactionKey], interactionKey).toBeDefined();
    }
  });

  it("round-trips through JSON serialization without losing graph shape", () => {
    const workflowConfig = loadOrgTransferWorkflowConfig();
    const graph = requireGraph(workflowConfig);
    const serializedConfig = JSON.stringify(workflowConfig);
    const deserializedConfig = JSON.parse(serializedConfig) as WorkflowConfig;
    const deserializedGraph = requireGraph(deserializedConfig);

    expect(deserializedGraph.startNodeId).toBe(graph.startNodeId);
    expect(nodeIds(deserializedGraph.nodes)).toEqual(nodeIds(graph.nodes));
  });
});

function loadOrgTransferWorkflowConfig(): WorkflowConfig {
  const workflowConfigResult = getWorkflowConfigByIntent(orgTransferIntent);

  expect(workflowConfigResult.ok).toBe(true);
  if (!workflowConfigResult.ok) {
    throw new Error("Missing org transfer workflow config.");
  }

  return workflowConfigResult.value;
}

function requireGraph(workflowConfig: WorkflowConfig) {
  expect(workflowConfig.graph).toBeDefined();
  if (workflowConfig.graph === undefined) {
    throw new Error("Missing workflow graph.");
  }

  return workflowConfig.graph;
}

function nodeIds(nodes: WorkflowGraphNodeConfig[]): string[] {
  return nodes.map((node) => {
    return node.nodeId;
  });
}

function outcomeKeys(node: WorkflowGraphNodeConfig): string[] {
  return (node.outcomes ?? []).map((outcome) => {
    return outcome.outcome;
  });
}

function contextOutputKeys(employeeContext: unknown[]): string[] {
  return employeeContext.map((contextItem) => {
    return String(requireRecord(contextItem)["outputKey"]);
  });
}

function requireRecord(value: unknown): Record<string, unknown> {
  expect(typeof value).toBe("object");
  expect(value).not.toBeNull();
  expect(Array.isArray(value)).toBe(false);

  return value as Record<string, unknown>;
}

function requireArray(value: unknown): unknown[] {
  expect(Array.isArray(value)).toBe(true);

  return value as unknown[];
}

function requireStringArray(value: unknown): string[] {
  const values = requireArray(value);

  return values.map((item) => {
    return String(item);
  });
}
