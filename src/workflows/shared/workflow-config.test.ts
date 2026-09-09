import { describe, expect, it } from "vitest";
import {
  LEDGER_EVENT_TYPES,
  WORKFLOW_INTENTS,
  type WorkflowState,
} from "@human-capital-management-suite/foundation";
import {
  getWorkflowConfigByIntent,
  type WorkflowApprovalGateApproverResolverConfig,
  type WorkflowApprovalGateConfig,
  type WorkflowApprovalGateFailurePolicyConfig,
  type WorkflowApprovalGatePassRuleConfig,
  type WorkflowConfig,
  type WorkflowGraphNodeConfig,
} from "./workflow-config.js";

const orgTransferIntent = WORKFLOW_INTENTS.EMPLOYEE_ORG_TRANSFER_COMPENSATION_CHANGE;
const headcountIntent = WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL;

const lowerSnakeCasePattern = /^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$/;

const knownActionActors = new Set([
  "requester",
  "initiator",
  "hr_admin",
  "source_manager",
  "destination_manager",
  "finance_admin",
  "hrbp",
  "compensation_admin",
  "medical_director",
  "clinic_ops_admin",
  "org_transfer_approver",
  "approval_task_assignee",
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

const knownApprovalGateModes = new Set(["sequential", "parallel"]);

const knownApprovalGateResolverTypes = new Set([
  "actor",
  "role",
  "manager_chain",
  "department_lead",
  "cost_center_owner",
  "seniority_level",
  "workflow_field",
]);

const knownApprovalGatePassRuleTypes = new Set([
  "all_required",
  "quorum",
  "percentage",
  "any_one",
  "weighted",
  "role_quorum",
  "composite",
]);

const knownApprovalGateFailurePolicyTypes = new Set([
  "stop_workflow",
  "send_to_repair",
  "continue_until_threshold_impossible",
  "require_all_responses",
  "veto_only",
  "escalate_on_timeout",
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

describe("position headcount requisition workflow config", () => {
  it("is registered by intent with HR and clinic ops start actors", () => {
    const workflowConfig = loadHeadcountWorkflowConfig();

    expect(workflowConfig.intent).toBe(headcountIntent);
    expect(workflowConfig.subjectType).toBe("position");
    expect(workflowConfig.selfServiceStart).toBe(false);
    expect(workflowConfig.startActors).toEqual(["hr_admin", "clinic_ops_admin"]);
  });

  it("has one start node and a valid approval-gate graph contract", () => {
    const workflowConfig = loadHeadcountWorkflowConfig();
    const graph = requireGraph(workflowConfig);
    const nodesById = nodesByNodeId(workflowConfig);
    const stateKeys = new Set(Object.keys(workflowConfig.states));
    const interactionKeys = new Set(Object.keys(workflowConfig.interactions));
    const startNodes = graph.nodes.filter((node) => {
      return node.nodeId === graph.startNodeId;
    });
    const gateNodes = graph.nodes.filter((node) => {
      return node.type === "approval_gate";
    });

    expect(startNodes).toHaveLength(1);
    expect(nodeIds(gateNodes)).toEqual([
      "leadership_chain_gate",
      "cross_functional_gate",
    ]);

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
      }
    }

    for (const gateNode of gateNodes) {
      const approvalGate = requireApprovalGate(gateNode);

      expect(knownApprovalGateModes.has(approvalGate.mode), gateNode.nodeId).toBe(true);
      expect(interactionKeys.has(approvalGate.interaction), gateNode.nodeId).toBe(true);
      expect(approvalGate.snapshotResolvedApprovers, gateNode.nodeId).toBe(true);
      expect(approvalGate.taskVersionRequired, gateNode.nodeId).toBe(true);
      expect(approvalGate.approverResolvers.length, gateNode.nodeId).toBeGreaterThan(0);

      validateApprovalGateOutcomes(gateNode, nodesById);
      validateApproverResolvers(approvalGate.approverResolvers);
      validatePassRule(approvalGate.passRule);
      validateFailurePolicies({
        policies: approvalGate.failurePolicies,
        nodesById,
        stateKeys,
        interactionKeys,
      });
    }
  });

  it("declares the expected sequential and parallel gate shapes", () => {
    const workflowConfig = loadHeadcountWorkflowConfig();
    const leadershipGate = requireApprovalGate(
      requireGraphNode(workflowConfig, "leadership_chain_gate"),
    );
    const crossFunctionalGate = requireApprovalGate(
      requireGraphNode(workflowConfig, "cross_functional_gate"),
    );
    const leadershipResolver = leadershipGate.approverResolvers[0];

    expect(leadershipGate.mode).toBe("sequential");
    expect(leadershipGate.passRule.type).toBe("all_required");
    expect(leadershipResolver?.type).toBe("workflow_field");
    expect(leadershipResolver?.fieldPath).toBe(
      "submitInput.selectedLeadershipApprovers",
    );
    expect(leadershipResolver?.preserveOrder).toBe(true);

    expect(crossFunctionalGate.mode).toBe("parallel");
    expect(crossFunctionalGate.approverResolvers).toHaveLength(5);
    expect(
      crossFunctionalGate.approverResolvers.every((resolver) => {
        return resolver.opensWithGate === true;
      }),
    ).toBe(true);
    expect(crossFunctionalGate.passRule).toEqual({
      type: "quorum",
      requiredApprovals: 3,
      eligibleApprovals: 5,
    });
    expect(
      crossFunctionalGate.approverResolvers
        .filter((resolver) => {
          return resolver.isVetoHolder === true;
        })
        .map((resolver) => {
          return resolver.resolverId;
        }),
    ).toEqual(["finance_reviewer", "medical_director_reviewer"]);
  });

  it("includes required intake fields and execution/repair interactions", () => {
    const workflowConfig = loadHeadcountWorkflowConfig();
    const inputInteraction = requireRecord(workflowConfig.interactions["input"]);
    const jsonSchema = requireRecord(inputInteraction["jsonSchema"]);
    const requiredFields = requireStringArray(jsonSchema["required"]);

    expect(requiredFields).toEqual(
      expect.arrayContaining([
        "department",
        "team",
        "location",
        "costCenter",
        "jobCode",
        "title",
        "level",
        "requestedFte",
        "targetStartDate",
        "salaryRangeMin",
        "salaryRangeMax",
        "businessJustification",
        "selectedLeadershipApprovers",
      ]),
    );

    for (const interactionKey of [
      "leadershipApproval",
      "crossFunctionalApproval",
      "readyToExecute",
      "repair",
      "completedSummary",
    ]) {
      expect(workflowConfig.interactions[interactionKey], interactionKey).toBeDefined();
    }
  });

  it("uses known actors, handlers, states, and ledger events", () => {
    const workflowConfig = loadHeadcountWorkflowConfig();
    const ledgerEventTypes = new Set<string>(Object.values(LEDGER_EVENT_TYPES));

    assertKnownActions(workflowConfig);

    for (const ledgerEventName of collectLedgerEventNames(workflowConfig)) {
      expect(ledgerEventTypes.has(ledgerEventName), ledgerEventName).toBe(true);
    }
  });

  it("round-trips approval-gate nodes through JSON serialization", () => {
    const workflowConfig = loadHeadcountWorkflowConfig();
    const serializedConfig = JSON.stringify(workflowConfig);
    const deserializedConfig = JSON.parse(serializedConfig) as WorkflowConfig;
    const deserializedGateNodes = requireGraph(deserializedConfig).nodes.filter(
      (node) => {
        return node.type === "approval_gate";
      },
    );

    expect(nodeIds(deserializedGateNodes)).toEqual([
      "leadership_chain_gate",
      "cross_functional_gate",
    ]);
    expect(
      requireApprovalGate(requireGraphNode(deserializedConfig, "cross_functional_gate"))
        .passRule,
    ).toEqual({
      type: "quorum",
      requiredApprovals: 3,
      eligibleApprovals: 5,
    });
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

function loadHeadcountWorkflowConfig(): WorkflowConfig {
  const workflowConfigResult = getWorkflowConfigByIntent(headcountIntent);

  expect(workflowConfigResult.ok).toBe(true);
  if (!workflowConfigResult.ok) {
    throw new Error("Missing headcount requisition workflow config.");
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

function nodesByNodeId(
  workflowConfig: WorkflowConfig,
): Map<string, WorkflowGraphNodeConfig> {
  return new Map(
    requireGraph(workflowConfig).nodes.map((node) => {
      return [node.nodeId, node];
    }),
  );
}

function requireGraphNode(
  workflowConfig: WorkflowConfig,
  nodeId: string,
): WorkflowGraphNodeConfig {
  const graphNode = nodesByNodeId(workflowConfig).get(nodeId);

  expect(graphNode, nodeId).toBeDefined();
  if (graphNode === undefined) {
    throw new Error(`Missing workflow graph node ${nodeId}.`);
  }

  return graphNode;
}

function requireApprovalGate(
  node: WorkflowGraphNodeConfig,
): WorkflowApprovalGateConfig {
  expect(node.type, node.nodeId).toBe("approval_gate");
  expect(node.approvalGate, node.nodeId).toBeDefined();
  if (node.approvalGate === undefined) {
    throw new Error(`Missing approval gate config for ${node.nodeId}.`);
  }

  return node.approvalGate;
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

function assertKnownActions(workflowConfig: WorkflowConfig): void {
  const stateKeys = new Set(Object.keys(workflowConfig.states));

  for (const [state, stateConfig] of Object.entries(workflowConfig.states)) {
    expect(stateKeys.has(state), `${state} exists`).toBe(true);

    for (const action of stateConfig.actions) {
      expect(knownActionActors.has(action.actor), `${state} actor`).toBe(true);
      expect(knownActionHandlers.has(action.handler), `${state} handler`).toBe(true);
      expect(action.transition.trim().length, `${state} transition`).toBeGreaterThan(0);

      if (action.nextState !== undefined) {
        expect(
          stateKeys.has(action.nextState as WorkflowState),
          `${state} nextState`,
        ).toBe(true);
      }
    }
  }
}

function validateApprovalGateOutcomes(
  gateNode: WorkflowGraphNodeConfig,
  nodesById: Map<string, WorkflowGraphNodeConfig>,
): void {
  const outcomes = gateNode.outcomes ?? [];
  const outcomeKeysForGate = new Set(
    outcomes.map((outcome) => {
      return outcome.outcome;
    }),
  );

  expect(outcomeKeysForGate.has("gate_passed"), `${gateNode.nodeId} pass`).toBe(true);
  expect(outcomeKeysForGate.has("gate_failed"), `${gateNode.nodeId} fail`).toBe(true);

  for (const outcome of outcomes) {
    expect(
      lowerSnakeCasePattern.test(outcome.outcome),
      `${gateNode.nodeId}.${outcome.outcome}`,
    ).toBe(true);

    if (outcome.outcome === "gate_passed" || outcome.outcome === "gate_failed") {
      expect(outcome.nextNodeId, `${gateNode.nodeId}.${outcome.outcome}`).toBeDefined();
      if (outcome.nextNodeId === undefined) {
        continue;
      }

      expect(
        nodesById.has(outcome.nextNodeId),
        `${gateNode.nodeId}.${outcome.outcome} nextNodeId`,
      ).toBe(true);
    }
  }
}

function validateApproverResolvers(
  resolvers: WorkflowApprovalGateApproverResolverConfig[],
): void {
  for (const resolver of resolvers) {
    expect(resolver.resolverId.trim().length, resolver.resolverId).toBeGreaterThan(0);
    expect(knownApprovalGateResolverTypes.has(resolver.type), resolver.resolverId).toBe(
      true,
    );
    expect(resolver.taskKey.trim().length, resolver.resolverId).toBeGreaterThan(0);
    expect(resolver.approvalType.trim().length, resolver.resolverId).toBeGreaterThan(0);
    expect(resolver.permission.trim().length, resolver.resolverId).toBeGreaterThan(0);

    if (resolver.type === "actor") {
      expect(resolver.actorId?.trim().length, resolver.resolverId).toBeGreaterThan(0);
    }

    if (resolver.type === "role") {
      expect(resolver.role?.trim().length, resolver.resolverId).toBeGreaterThan(0);
    }

    if (resolver.type === "manager_chain") {
      expect(resolver.subjectPath?.trim().length, resolver.resolverId).toBeGreaterThan(
        0,
      );
    }

    if (resolver.type === "department_lead") {
      expect(
        resolver.departmentPath?.trim().length,
        resolver.resolverId,
      ).toBeGreaterThan(0);
    }

    if (resolver.type === "cost_center_owner") {
      expect(
        resolver.costCenterPath?.trim().length,
        resolver.resolverId,
      ).toBeGreaterThan(0);
    }

    if (resolver.type === "seniority_level") {
      expect(
        resolver.seniorityLevelPath?.trim().length,
        resolver.resolverId,
      ).toBeGreaterThan(0);
    }

    if (resolver.type === "workflow_field") {
      expect(resolver.fieldPath?.trim().length, resolver.resolverId).toBeGreaterThan(0);
    }

    if (resolver.weight !== undefined) {
      expect(resolver.weight, resolver.resolverId).toBeGreaterThan(0);
    }
  }
}

function validatePassRule(passRule: WorkflowApprovalGatePassRuleConfig): void {
  expect(knownApprovalGatePassRuleTypes.has(passRule.type), passRule.type).toBe(true);

  if (passRule.type === "quorum") {
    expect(passRule.requiredApprovals).toBeGreaterThan(0);
    expect(passRule.eligibleApprovals).toBeGreaterThanOrEqual(
      passRule.requiredApprovals,
    );
  }

  if (passRule.type === "percentage") {
    expect(passRule.requiredPercentage).toBeGreaterThan(0);
    expect(passRule.requiredPercentage).toBeLessThanOrEqual(100);
    expect(passRule.eligibleApprovals).toBeGreaterThan(0);
  }

  if (passRule.type === "weighted") {
    expect(passRule.requiredWeight).toBeGreaterThan(0);
    expect(passRule.totalWeight).toBeGreaterThanOrEqual(passRule.requiredWeight);
  }

  if (passRule.type === "role_quorum") {
    expect(passRule.roleQuorums.length).toBeGreaterThan(0);

    for (const roleQuorum of passRule.roleQuorums) {
      expect(roleQuorum.role.trim().length).toBeGreaterThan(0);
      expect(roleQuorum.requiredApprovals).toBeGreaterThan(0);
      expect(roleQuorum.eligibleApprovals).toBeGreaterThanOrEqual(
        roleQuorum.requiredApprovals,
      );
    }
  }

  if (passRule.type === "composite") {
    expect(passRule.rules.length).toBeGreaterThan(0);

    for (const childRule of passRule.rules) {
      validatePassRule(childRule);
    }
  }
}

function validateFailurePolicies(input: {
  policies: WorkflowApprovalGateFailurePolicyConfig[];
  nodesById: Map<string, WorkflowGraphNodeConfig>;
  stateKeys: Set<string>;
  interactionKeys: Set<string>;
}): void {
  expect(input.policies.length).toBeGreaterThan(0);

  for (const policy of input.policies) {
    expect(knownApprovalGateFailurePolicyTypes.has(policy.type), policy.type).toBe(
      true,
    );

    if (policy.nextNodeId !== undefined) {
      expect(input.nodesById.has(policy.nextNodeId), policy.type).toBe(true);
    }

    if (policy.nextState !== undefined) {
      expect(input.stateKeys.has(policy.nextState), policy.type).toBe(true);
    }

    if (policy.nextInteraction !== undefined) {
      expect(input.interactionKeys.has(policy.nextInteraction), policy.type).toBe(true);
    }

    if (policy.type === "escalate_on_timeout") {
      expect(policy.timeoutAfter?.trim().length, policy.type).toBeGreaterThan(0);
      expect(policy.escalationResolverId?.trim().length, policy.type).toBeGreaterThan(
        0,
      );
    }
  }
}

function collectLedgerEventNames(workflowConfig: WorkflowConfig): string[] {
  const ledgerEventNames = new Set<string>();

  for (const eventName of workflowConfig.submit.additionalEvents) {
    ledgerEventNames.add(eventName);
  }

  for (const eventName of workflowConfig.timeline.businessEvents) {
    ledgerEventNames.add(eventName);
  }

  for (const eventName of Object.keys(workflowConfig.timeline.summaries)) {
    ledgerEventNames.add(eventName);
  }

  for (const node of requireGraph(workflowConfig).nodes) {
    for (const outcome of node.outcomes ?? []) {
      if (outcome.eventType !== undefined) {
        ledgerEventNames.add(outcome.eventType);
      }
    }

    if (node.type === "approval_gate") {
      const approvalGate = requireApprovalGate(node);

      for (const eventName of Object.values(approvalGate.events)) {
        if (eventName !== undefined) {
          ledgerEventNames.add(eventName);
        }
      }
    }
  }

  return [...ledgerEventNames];
}
