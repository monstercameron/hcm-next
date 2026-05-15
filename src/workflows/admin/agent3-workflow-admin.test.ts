import { describe, expect, it } from "vitest";
import {
  ACTOR_ROLES,
  LEDGER_EVENT_TYPES,
  PERMISSION_KEYS,
  WORKFLOW_INTENTS,
  ok,
} from "@hcm-next/foundation";
import type {
  ActorRecord,
  ApprovalTaskRecord,
  EmployeeProjectionRecord,
  WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import { diffWorkflowConfigs } from "./diff/workflow-diff.js";
import { runPublishGuardrails } from "./guardrails/publish-guardrails.js";
import {
  validateIntegrationBindings,
  type TenantConnectorBinding,
} from "./integrations/integration-bindings.js";
import { previewWorkflowPermissions } from "./permissions/permission-preview.js";
import { validateApprovalGateAdminConfig } from "./safety/approval-gate-validation.js";
import { simulateWorkflow } from "./simulation/workflow-simulator.js";
import {
  getWorkflowConfigByIntent,
  type WorkflowApprovalGateConfig,
  type WorkflowConfig,
  type WorkflowGraphNodeConfig,
} from "../shared/workflow-config.js";
import type { EmployeeAccessEvaluationContext } from "../shared/employee-access.js";

type ResultValue<TResult> = TResult extends { ok: true; value: infer TValue }
  ? TValue
  : never;

describe("Agent 3 workflow admin helpers", () => {
  it("previews employee field visibility, actions, and AI-redacted paths", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const employeeProjection = employeeProjectionFixture();
    const actor = employeeActorFixture();
    const previewResult = previewWorkflowPermissions({
      tenantId: "tenant_demo",
      environmentId: "env_demo",
      actor,
      employeeProjection,
      workflowConfig,
      workflowState: "collecting_input",
      accessContext: {
        legacyGrants: [
          {
            grantId: "grant_self_profile_workflow",
            actorIds: [actor.actorId],
            fieldGroups: ["profile", "workflow"],
            scopes: [{ type: "self" }],
          },
        ],
      },
    });

    expect(previewResult.ok).toBe(true);
    if (!previewResult.ok) {
      return;
    }

    expect(previewResult.value.relationships.self).toBe(true);
    expect(previewResult.value.aiVisibleFieldGroups).toEqual(["profile", "workflow"]);
    expect(previewResult.value.aiRedactedPaths).toContain("compensation");
    expect(
      previewResult.value.availableActions.map((action) => action.transition),
    ).toContain("submit_input");
  });

  it("previews direct manager, HR, compensation, finance, and unrelated access", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const employeeProjection = employeeProjectionFixture();
    const directManager = roleActorFixture("actor_manager", ACTOR_ROLES.MANAGER);
    const hrAdmin = hrAdminActorFixture();
    const compensationAdmin = roleActorFixture(
      "actor_comp",
      ACTOR_ROLES.COMPENSATION_ADMIN,
    );
    const financeAdmin = roleActorFixture("actor_finance", ACTOR_ROLES.FINANCE_ADMIN);
    const unrelatedEmployee = {
      ...employeeActorFixture(),
      actorId: "actor_unrelated",
      linkedWorkerId: "emp_unrelated",
    };

    const managerPreview = unwrapPreview(
      previewWorkflowPermissions({
        tenantId: "tenant_demo",
        environmentId: "env_demo",
        actor: {
          ...directManager,
          linkedWorkerId: "emp_manager",
        },
        employeeProjection,
        workflowConfig,
        workflowState: "collecting_input",
        accessContext: accessContextFor([
          {
            grantId: "grant_manager_reports",
            actorIds: [directManager.actorId],
            fieldGroups: ["profile", "organization", "job", "workflow"],
            scopes: [{ type: "direct_reports" }],
          },
        ]),
      }),
    );
    const hrPreview = unwrapPreview(
      previewWorkflowPermissions({
        tenantId: "tenant_demo",
        environmentId: "env_demo",
        actor: hrAdmin,
        employeeProjection,
        workflowConfig,
        workflowState: "collecting_input",
        accessContext: accessContextFor([
          {
            grantId: "grant_hr_global",
            roles: [ACTOR_ROLES.HR_ADMIN],
            fieldGroups: [
              "profile",
              "organization",
              "job",
              "employment",
              "contact",
              "workflow",
            ],
            scopes: [{ type: "global" }],
          },
        ]),
      }),
    );
    const compensationPreview = unwrapPreview(
      previewWorkflowPermissions({
        tenantId: "tenant_demo",
        environmentId: "env_demo",
        actor: compensationAdmin,
        employeeProjection,
        workflowConfig,
        workflowState: "waiting_approval",
        accessContext: accessContextFor([
          {
            grantId: "grant_compensation_global",
            roles: [ACTOR_ROLES.COMPENSATION_ADMIN],
            fieldGroups: ["profile", "job", "organization", "compensation", "workflow"],
            scopes: [{ type: "global" }],
          },
        ]),
        pendingTasks: [
          approvalTaskFixture({
            assigneeActorId: compensationAdmin.actorId,
            assigneeRole: ACTOR_ROLES.COMPENSATION_ADMIN,
          }),
        ],
      }),
    );
    const financePreview = unwrapPreview(
      previewWorkflowPermissions({
        tenantId: "tenant_demo",
        environmentId: "env_demo",
        actor: financeAdmin,
        employeeProjection,
        workflowConfig,
        workflowState: "waiting_approval",
        accessContext: accessContextFor([
          {
            grantId: "grant_finance_global",
            roles: [ACTOR_ROLES.FINANCE_ADMIN],
            fieldGroups: ["profile", "organization", "compensation", "workflow"],
            scopes: [{ type: "global" }],
          },
        ]),
        pendingTasks: [
          approvalTaskFixture({
            assigneeActorId: financeAdmin.actorId,
            assigneeRole: ACTOR_ROLES.FINANCE_ADMIN,
          }),
        ],
      }),
    );
    const unrelatedPreview = unwrapPreview(
      previewWorkflowPermissions({
        tenantId: "tenant_demo",
        environmentId: "env_demo",
        actor: unrelatedEmployee,
        employeeProjection,
        workflowConfig,
        workflowState: "collecting_input",
        workflowInstance: permissionPreviewWorkflowInstance({
          requesterActorId: "actor_original_requester",
          state: "collecting_input",
        }),
        accessContext: accessContextFor([]),
      }),
    );

    expect(managerPreview.relationships.manager).toBe(true);
    expect(visibleFieldGroups(managerPreview)).toEqual(
      expect.arrayContaining(["profile", "organization", "job", "workflow"]),
    );
    expect(hrPreview.relationships.orgAdmin).toBe(true);
    expect(visibleFieldGroups(hrPreview)).not.toContain("compensation");
    expect(compensationPreview.relationships.compensationAdmin).toBe(true);
    expect(visibleFieldGroups(compensationPreview)).toContain("compensation");
    expect(compensationPreview.approvalAuthority).toBe(true);
    expect(financePreview.relationships.financeApprover).toBe(true);
    expect(visibleFieldGroups(financePreview)).toContain("compensation");
    expect(financePreview.approvalAuthority).toBe(true);
    expect(unrelatedPreview.relationships.self).toBe(false);
    expect(unrelatedPreview.aiVisibleFieldGroups).toEqual([]);
    expect(unrelatedPreview.availableActions).toEqual([]);
  });

  it("simulates the compensation workflow accepted vendor path without durable writes", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const employeeProjection = employeeProjectionFixture();
    const simulationResult = simulateWorkflow({
      workflowConfig,
      actor: hrAdminActorFixture(),
      employeeProjection: employeeProjection.document,
      workflowInput: {
        proposedCompensation: {
          amount: 125000,
          currency: "USD",
          payFrequency: "annual",
          bonusTargetPercent: 15,
          effectiveDate: "2026-06-01",
        },
        effectiveAt: "2026-06-01",
        businessReason: "market_adjustment",
      },
      approvalDecisions: {
        compensation_approval: "approved",
      },
      fakeIntegrationResponses: {
        vendor_compensation_decision: {
          status: "accepted",
          decisionId: "decision_accepted",
        },
      },
    });

    expect(simulationResult.ok).toBe(true);
    if (!simulationResult.ok) {
      return;
    }

    expect(simulationResult.value.durableWritesCreated).toBe(false);
    expect(simulationResult.value.finalNodeId).toBe("completed");
    expect(simulationResult.value.externalCallPreview).toHaveLength(1);
    expect(
      simulationResult.value.proposedLedgerEvents.map((event) => event["eventType"]),
    ).toContain("ExternalWriteSucceeded");
  });

  it("simulates legal name, contact, emergency, compensation, org-transfer, and headcount fixtures", () => {
    const employeeProjection = employeeProjectionFixture();
    const legalWorkflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const contactWorkflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_CONTACT_INFO_UPDATE,
    );
    const emergencyWorkflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_EMERGENCY_CONTACT_UPDATE,
    );
    const compensationWorkflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const orgTransferWorkflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_ORG_TRANSFER_COMPENSATION_CHANGE,
    );
    const headcountWorkflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL,
    );

    const legalHappyPath = unwrapSimulation(
      simulateWorkflow({
        workflowConfig: legalWorkflowConfig,
        actor: employeeActorFixture(),
        employeeProjection: employeeProjection.document,
        workflowInput: legalNameInputFixture(),
        approvalDecisions: {
          hr_legal_name_approval: "approved",
        },
      }),
    );
    const legalMissingEvidence = unwrapSimulation(
      simulateWorkflow({
        workflowConfig: legalWorkflowConfig,
        actor: employeeActorFixture(),
        employeeProjection: employeeProjection.document,
        workflowInput: legalNameInputFixture(),
        blockExecutor: ({ node }) =>
          ok({
            routeKey: node.nodeId === "legal_name_preflight" ? "invalid" : "valid",
            output: { nodeId: node.nodeId },
            validationErrors:
              node.nodeId === "legal_name_preflight"
                ? [{ code: "legal_name.evidence_missing" }]
                : [],
          }),
      }),
    );
    const contactWarning = unwrapSimulation(
      simulateWorkflow({
        workflowConfig: contactWorkflowConfig,
        actor: employeeActorFixture(),
        employeeProjection: employeeProjection.document,
        workflowInput: contactInfoInputFixture(),
        blockExecutor: ({ node }) =>
          ok({
            routeKey: "valid",
            output: { nodeId: node.nodeId },
            warnings:
              node.nodeId === "contact_info_preflight"
                ? [{ code: "contact_info.country_region_mismatch" }]
                : [],
          }),
      }),
    );
    const emergencyDuplicateWarning = unwrapSimulation(
      simulateWorkflow({
        workflowConfig: emergencyWorkflowConfig,
        actor: employeeActorFixture(),
        employeeProjection: employeeProjection.document,
        workflowInput: emergencyContactInputFixture(),
        blockExecutor: ({ node }) =>
          ok({
            routeKey: "valid",
            output: { nodeId: node.nodeId },
            warnings:
              node.nodeId === "emergency_contact_preflight"
                ? [{ code: "emergency_contact.duplicate_candidate" }]
                : [],
          }),
      }),
    );
    const compensationRejected = unwrapSimulation(
      simulateWorkflow({
        workflowConfig: compensationWorkflowConfig,
        actor: hrAdminActorFixture(),
        employeeProjection: employeeProjection.document,
        workflowInput: compensationInputFixture(),
        approvalDecisions: {
          compensation_approval: "approved",
        },
        fakeIntegrationResponses: {
          vendor_compensation_decision: {
            status: "rejected",
            reason: "outside_market_guardrail",
          },
        },
      }),
    );
    const orgTransferAccessImpact = unwrapSimulation(
      simulateWorkflow({
        workflowConfig: orgTransferWorkflowConfig,
        actor: hrAdminActorFixture(),
        employeeProjection: employeeProjection.document,
        workflowInput: orgTransferInputFixture(),
        nodeOutcomeOverrides: {
          check_source_manager_visibility: "allowed",
          check_destination_manager_visibility: "allowed",
          check_finance_cost_center_scope: "allowed",
          check_compensation_scope: "allowed",
          check_medical_director_clinical_scope: "allowed",
        },
        approvalDecisions: {
          source_manager_approval: "approved",
          destination_manager_approval: "approved",
          finance_approval: "approved",
          compensation_approval: "approved",
          medical_director_approval: "approved",
        },
      }),
    );
    const headcountApprovalGates = unwrapSimulation(
      simulateWorkflow({
        workflowConfig: headcountWorkflowConfig,
        actor: hrAdminActorFixture(),
        employeeProjection: employeeProjection.document,
        workflowInput: headcountInputFixture(),
        approvalDecisions: {
          leadership_chain_gate: "gate_passed",
          cross_functional_gate: "gate_passed",
        },
      }),
    );

    expect(legalHappyPath.finalNodeId).toBe("completed");
    expect(legalHappyPath.proposedLedgerEvents).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          eventType: LEDGER_EVENT_TYPES.PERSON_LEGAL_NAME_CHANGED,
        }),
      ]),
    );
    expect(legalMissingEvidence.validationErrors).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ code: "legal_name.evidence_missing" }),
      ]),
    );
    expect(contactWarning.warnings).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ code: "contact_info.country_region_mismatch" }),
      ]),
    );
    expect(emergencyDuplicateWarning.warnings).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ code: "emergency_contact.duplicate_candidate" }),
      ]),
    );
    expect(compensationRejected.finalNodeId).toBe("canceled");
    expect(compensationRejected.routeDecisions).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          routeKey: "rejected",
          nextNodeId: "repair_vendor_decision",
        }),
      ]),
    );
    expect(orgTransferAccessImpact.traces.map((trace) => trace.nodeId)).toEqual(
      expect.arrayContaining([
        "check_source_manager_visibility",
        "check_destination_manager_visibility",
        "check_finance_cost_center_scope",
      ]),
    );
    expect(headcountApprovalGates.traces).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          nodeId: "leadership_chain_gate",
          approvalGatePreview: expect.objectContaining({ mode: "sequential" }),
        }),
        expect.objectContaining({
          nodeId: "cross_functional_gate",
          approvalGatePreview: expect.objectContaining({ mode: "parallel" }),
        }),
      ]),
    );
  });

  it("includes simulator permission checks and approval gate previews", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL,
    );
    const simulationResult = unwrapSimulation(
      simulateWorkflow({
        workflowConfig,
        actor: hrAdminActorFixture(),
        employeeProjection: employeeProjectionFixture().document,
        workflowInput: headcountInputFixture(),
        approvalDecisions: {
          leadership_chain_gate: "gate_passed",
          cross_functional_gate: "gate_passed",
        },
      }),
    );

    expect(simulationResult.traces).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          nodeId: "headcount_intake",
          permissionCheck: expect.objectContaining({
            allowedTransitions: expect.arrayContaining(["submit_input"]),
          }),
        }),
        expect.objectContaining({
          nodeId: "leadership_chain_gate",
          approvalGatePreview: expect.objectContaining({
            gateId: "leadership_chain_gate",
            selectedDecision: "gate_passed",
          }),
        }),
      ]),
    );
  });

  it("classifies new external calls and compensation path changes as high risk", () => {
    const publishedConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const draftConfig = cloneWorkflowConfig(publishedConfig);
    const graph = requireGraph(draftConfig);

    graph.nodes.push({
      nodeId: "notify_external_vendor",
      type: "external_write",
      title: "Notify compensation vendor",
      connectionId: "compensation_vendor",
      operation: "notifyCompensationChange",
      outcomes: [
        {
          outcome: "accepted",
          routeKey: "accepted",
          nextNodeId: "completed",
        },
      ],
    });

    const diffResult = diffWorkflowConfigs({ draftConfig, publishedConfig });

    expect(diffResult.ok).toBe(true);
    if (!diffResult.ok) {
      return;
    }

    expect(diffResult.value.impact.riskLevel).toBe("high");
    expect(diffResult.value.impact.newExternalCallCount).toBeGreaterThan(0);
    expect(diffResult.value.impact.compensationPathChangeCount).toBeGreaterThan(0);
  });

  it("classifies no-op, route, approval removal, and sensitive permission diffs", () => {
    const legalNameConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const headcountConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL,
    );
    const routeChangeConfig = cloneWorkflowConfig(legalNameConfig);
    const approvalRemovalConfig = cloneWorkflowConfig(headcountConfig);
    const fieldExposureConfig = cloneWorkflowConfig(headcountConfig);
    const routeNode = requireNode(routeChangeConfig, "legal_name_preflight");
    const approvalRemovalGraph = requireGraph(approvalRemovalConfig);
    const fieldExposureGate = requireApprovalGateConfig(
      fieldExposureConfig,
      "cross_functional_gate",
    );

    routeNode.outcomes = (routeNode.outcomes ?? []).map((outcome) => {
      return outcome.routeKey === "valid"
        ? {
            ...outcome,
            nextNodeId: "hr_legal_name_approval",
          }
        : outcome;
    });
    routeChangeConfig.graph!.edges = (routeChangeConfig.graph!.edges ?? []).map(
      (edge) => {
        return edge.fromNodeId === "legal_name_preflight" && edge.routeKey === "valid"
          ? {
              ...edge,
              toNodeId: "hr_legal_name_approval",
            }
          : edge;
      },
    );
    approvalRemovalGraph.nodes = approvalRemovalGraph.nodes.filter((node) => {
      return node.nodeId !== "cross_functional_gate";
    });
    fieldExposureGate.approverResolvers[0] = {
      ...fieldExposureGate.approverResolvers[0]!,
      permission: PERMISSION_KEYS.EMPLOYEE_VIEW_COMPENSATION,
    };

    const noOpDiff = unwrapDiff(
      diffWorkflowConfigs({
        draftConfig: legalNameConfig,
        publishedConfig: legalNameConfig,
      }),
    );
    const routeDiff = unwrapDiff(
      diffWorkflowConfigs({
        draftConfig: routeChangeConfig,
        publishedConfig: legalNameConfig,
      }),
    );
    const approvalRemovalDiff = unwrapDiff(
      diffWorkflowConfigs({
        draftConfig: approvalRemovalConfig,
        publishedConfig: headcountConfig,
      }),
    );
    const fieldExposureDiff = unwrapDiff(
      diffWorkflowConfigs({
        draftConfig: fieldExposureConfig,
        publishedConfig: headcountConfig,
      }),
    );

    expect(noOpDiff.hasChanges).toBe(false);
    expect(routeDiff.items).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          category: "route",
          reasonCodes: expect.arrayContaining(["route_target_changed"]),
        }),
      ]),
    );
    expect(approvalRemovalDiff.impact.removedApprovalStepCount).toBeGreaterThan(0);
    expect(approvalRemovalDiff.impact.riskLevel).toBe("high");
    expect(fieldExposureDiff.impact.sensitiveFieldExposureCount).toBeGreaterThan(0);
    expect(fieldExposureDiff.impact.riskLevel).toBe("high");
  });

  it("validates approval gate resolver and quorum safety", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL,
    );
    const gateConfig = requireApprovalGateConfig(
      workflowConfig,
      "cross_functional_gate",
    );
    const invalidGateConfig: WorkflowApprovalGateConfig = {
      ...gateConfig,
      passRule: {
        type: "quorum",
        requiredApprovals: 6,
        eligibleApprovals: 5,
      },
    };
    const validationResult = validateApprovalGateAdminConfig({
      gateConfig: invalidGateConfig,
      graph: requireGraph(workflowConfig),
      resolverFixture: {
        actors: [
          roleActorFixture("actor_finance", ACTOR_ROLES.FINANCE_ADMIN),
          roleActorFixture("actor_comp", ACTOR_ROLES.COMPENSATION_ADMIN),
        ],
      },
    });

    expect(validationResult.ok).toBe(true);
    if (!validationResult.ok) {
      return;
    }

    expect(validationResult.value.valid).toBe(false);
    expect(validationResult.value.errors.map((error) => error.code)).toContain(
      "approval_gate.quorum_impossible",
    );
  });

  it("validates ordered gates, quorum gates, vetoes, request-more-info, and task policies", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL,
    );
    const leadershipGate = requireApprovalGateConfig(
      workflowConfig,
      "leadership_chain_gate",
    );
    const crossFunctionalGate = requireApprovalGateConfig(
      workflowConfig,
      "cross_functional_gate",
    );
    const richPolicyGate: WorkflowApprovalGateConfig = {
      ...crossFunctionalGate,
      requestMoreInfoPolicy: {
        enabled: true,
        nextNodeId: "repair",
        nextState: "waiting_approval_repair",
        nextInteraction: "repair",
      },
      staleTaskPolicy: {
        after: "PT48H",
        action: "escalate",
        escalationResolverId: "finance_reviewer",
      },
      taskExpirationPolicy: {
        expiresAfter: "PT96H",
        expirationAction: "send_to_repair",
        nextNodeId: "repair",
      },
      delegationPolicy: {
        enabled: true,
        allowedResolverIds: ["finance_reviewer"],
        requiresAudit: true,
      },
    };
    const relationshipGate = gateWithResolvers([
      {
        resolverId: "manager_relationship",
        type: "relationship",
        label: "Manager relationship",
        taskKey: "manager",
        approvalType: "relationship_review",
        permission: PERMISSION_KEYS.ORG_TRANSFER_MANAGER_APPROVE,
        subjectPath: "approvers.managerIds",
      },
      {
        resolverId: "org_lead",
        type: "org_unit",
        label: "Org unit lead",
        taskKey: "org_lead",
        approvalType: "org_lead_review",
        permission: PERMISSION_KEYS.POSITION_HEADCOUNT_LEADERSHIP_APPROVE,
        departmentPath: "approvers.orgLeadIds",
      },
    ]);

    const leadershipValidation = unwrapApprovalGateValidation(
      validateApprovalGateAdminConfig({
        gateConfig: leadershipGate,
        graph: requireGraph(workflowConfig),
        resolverFixture: {
          actors: [roleActorFixture("actor_leader", "leader")],
          workflowContext: {
            submitInput: {
              selectedLeadershipApprovers: ["actor_leader"],
            },
          },
        },
      }),
    );
    const quorumValidation = unwrapApprovalGateValidation(
      validateApprovalGateAdminConfig({
        gateConfig: crossFunctionalGate,
        graph: requireGraph(workflowConfig),
        resolverFixture: {
          actors: [
            roleActorFixture("actor_finance_admin", ACTOR_ROLES.FINANCE_ADMIN),
            roleActorFixture("actor_hrbp", "hrbp"),
            roleActorFixture("actor_comp_admin", ACTOR_ROLES.COMPENSATION_ADMIN),
            roleActorFixture("actor_clinic_ops", "clinic_ops_admin"),
          ],
        },
      }),
    );
    const richPolicyValidation = unwrapApprovalGateValidation(
      validateApprovalGateAdminConfig({
        gateConfig: richPolicyGate,
        graph: requireGraph(workflowConfig),
        resolverFixture: {
          actors: [
            roleActorFixture("actor_finance_admin", ACTOR_ROLES.FINANCE_ADMIN),
            roleActorFixture("actor_hrbp", "hrbp"),
            roleActorFixture("actor_comp_admin", ACTOR_ROLES.COMPENSATION_ADMIN),
            roleActorFixture("actor_clinic_ops", "clinic_ops_admin"),
          ],
        },
      }),
    );
    const relationshipValidation = unwrapApprovalGateValidation(
      validateApprovalGateAdminConfig({
        gateConfig: relationshipGate,
        graph: requireGraph(workflowConfig),
        resolverFixture: {
          actors: [],
          workflowContext: {
            approvers: {
              managerIds: ["actor_manager"],
              orgLeadIds: ["actor_org_lead"],
            },
          },
        },
      }),
    );

    expect(leadershipValidation.valid).toBe(true);
    expect(leadershipValidation.resolvedApprovers[0]?.actorIds).toEqual([
      "actor_leader",
    ]);
    expect(quorumValidation.valid).toBe(true);
    expect(
      quorumValidation.resolvedApprovers.some((approver) => approver.isVetoHolder),
    ).toBe(true);
    expect(richPolicyValidation.errors).toEqual([]);
    expect(richPolicyValidation.valid).toBe(true);
    expect(relationshipValidation.valid).toBe(true);
    expect(
      relationshipValidation.resolvedApprovers.flatMap((item) => item.actorIds),
    ).toEqual(["actor_manager", "actor_org_lead"]);
  });

  it("rejects request-more-info routes that point to missing nodes", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL,
    );
    const gateConfig: WorkflowApprovalGateConfig = {
      ...requireApprovalGateConfig(workflowConfig, "leadership_chain_gate"),
      requestMoreInfoPolicy: {
        enabled: true,
        nextNodeId: "missing_repair_node",
      },
    };
    const validation = unwrapApprovalGateValidation(
      validateApprovalGateAdminConfig({
        gateConfig,
        graph: requireGraph(workflowConfig),
      }),
    );

    expect(validation.valid).toBe(false);
    expect(validation.errors.map((error) => error.code)).toContain(
      "approval_gate.request_more_info_next_node_missing",
    );
  });

  it("validates integration bindings and redacts secret details in previews", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const binding: TenantConnectorBinding = {
      abstractConnectionId: "third_party_compensation_decision",
      connectorId: "connector_comp_decision",
      environment: "sandbox",
      secretRef: "secret_missing",
      enabled: false,
      allowedOperations: ["submitCompensationChange"],
      timeoutMs: 5000,
      retryPolicy: {
        maxAttempts: 3,
        backoff: "exponential",
      },
      reconciliation: {
        required: true,
        expectedStatusPath: "status",
      },
      idempotencyScope: "node",
    };
    const validationResult = validateIntegrationBindings({
      workflowConfig,
      bindings: [binding],
      targetEnvironment: "production",
      availableSecretRefs: [],
    });

    expect(validationResult.ok).toBe(true);
    if (!validationResult.ok) {
      return;
    }

    expect(validationResult.value.valid).toBe(false);
    expect(validationResult.value.errors.map((error) => error.code)).toEqual(
      expect.arrayContaining([
        "integration_binding.disabled",
        "integration_binding.secret_missing",
        "integration_binding.production_points_to_sandbox",
      ]),
    );
    expect(validationResult.value.externalCallPreviews[0]?.secretRefStatus).toBe(
      "missing",
    );
  });

  it("rejects integration bindings that do not allow the configured operation", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const validationResult = validateIntegrationBindings({
      workflowConfig,
      bindings: [
        {
          abstractConnectionId: "third_party_compensation_decision",
          connectorId: "connector_comp_decision",
          environment: "production",
          secretRef: "secret_comp_decision",
          enabled: true,
          allowedOperations: ["readCompensationBands"],
          timeoutMs: 5000,
          retryPolicy: {
            maxAttempts: 3,
            backoff: "exponential",
          },
          reconciliation: {
            required: true,
            expectedStatusPath: "status",
          },
          idempotencyScope: "node",
        },
      ],
      targetEnvironment: "production",
      availableSecretRefs: ["secret_comp_decision"],
    });

    expect(validationResult.ok).toBe(true);
    if (!validationResult.ok) {
      return;
    }

    expect(validationResult.value.valid).toBe(false);
    expect(validationResult.value.errors.map((error) => error.code)).toContain(
      "integration_binding.operation_not_allowed",
    );
  });

  it("aggregates publish guardrails and returns every blocker together", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const guardrailResult = runPublishGuardrails({
      workflowConfig,
      availableBlocks: blockReferencesForConfig(workflowConfig),
      integrationBindings: {
        bindings: [],
        targetEnvironment: "production",
        availableSecretRefs: [],
      },
    });

    expect(guardrailResult.ok).toBe(true);
    if (!guardrailResult.ok) {
      return;
    }

    expect(guardrailResult.value.canPublish).toBe(false);
    expect(guardrailResult.value.errors.map((error) => error.code)).toEqual(
      expect.arrayContaining(["integration_binding.missing"]),
    );
  });

  it("blocks publishing sensitive field exposure without visibility rules", () => {
    const workflowConfig = cloneWorkflowConfig(
      loadWorkflowConfig(WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE),
    );

    workflowConfig.interactions["input"] = {
      ...workflowConfig.interactions["input"]!,
      employeeContext: [
        {
          outputKey: "visibleCompensationWithoutRules",
          path: "compensation",
        },
      ],
    };

    const guardrailResult = runPublishGuardrails({
      workflowConfig,
      availableBlocks: blockReferencesForConfig(workflowConfig),
      integrationBindings: validCompensationIntegrationBindings(),
    });

    expect(guardrailResult.ok).toBe(true);
    if (!guardrailResult.ok) {
      return;
    }

    expect(guardrailResult.value.canPublish).toBe(false);
    expect(guardrailResult.value.errors.map((error) => error.code)).toContain(
      "publish_guardrail.field_visibility_missing",
    );
  });

  it("proves publish warnings do not block unless guardrail policy creates errors", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL,
    );
    const guardrailResult = runPublishGuardrails({
      workflowConfig,
      availableBlocks: undefined,
      integrationBindings: validHeadcountIntegrationBindings(),
    });

    expect(guardrailResult.ok).toBe(true);
    if (!guardrailResult.ok) {
      return;
    }

    expect(guardrailResult.value.warnings.map((warning) => warning.code)).toContain(
      "publish_guardrail.block_catalog_missing",
    );
    expect(guardrailResult.value.errors).toEqual([]);
    expect(guardrailResult.value.canPublish).toBe(true);
  });
});

function loadWorkflowConfig(intent: string): WorkflowConfig {
  const workflowConfigResult = getWorkflowConfigByIntent(intent);

  expect(workflowConfigResult.ok).toBe(true);
  if (!workflowConfigResult.ok) {
    throw new Error(`Missing workflow config ${intent}.`);
  }

  return workflowConfigResult.value;
}

function requireGraph(
  workflowConfig: WorkflowConfig,
): NonNullable<WorkflowConfig["graph"]> {
  expect(workflowConfig.graph).toBeDefined();
  if (workflowConfig.graph === undefined) {
    throw new Error("Expected workflow graph.");
  }

  return workflowConfig.graph;
}

function requireApprovalGateConfig(
  workflowConfig: WorkflowConfig,
  nodeId: string,
): WorkflowApprovalGateConfig {
  const node = requireGraph(workflowConfig).nodes.find((candidate) => {
    return candidate.nodeId === nodeId;
  });

  expect(node?.approvalGate).toBeDefined();
  if (node?.approvalGate === undefined) {
    throw new Error(`Expected approval gate ${nodeId}.`);
  }

  return node.approvalGate;
}

function requireNode(
  workflowConfig: WorkflowConfig,
  nodeId: string,
): WorkflowGraphNodeConfig {
  const node = requireGraph(workflowConfig).nodes.find((candidate) => {
    return candidate.nodeId === nodeId;
  });

  expect(node).toBeDefined();
  if (node === undefined) {
    throw new Error(`Expected workflow node ${nodeId}.`);
  }

  return node;
}

function employeeProjectionFixture(): EmployeeProjectionRecord {
  return {
    tenantId: "tenant_demo",
    employeeId: "emp_jane",
    projectionVersion: 1,
    document: {
      employeeId: "emp_jane",
      person: {
        personId: "person_jane",
        legalName: {
          first: "Jane",
          middle: null,
          last: "Rivera",
        },
        displayName: "Jane Rivera",
        preferredName: null,
        workEmail: "jane@example.com",
      },
      contact: {
        personalEmail: "jane.personal@example.com",
        mobilePhone: "5550100",
        homeAddress: {
          line1: "1 Main",
          line2: null,
          city: "Boston",
          region: "MA",
          postalCode: "02110",
          country: "US",
        },
      },
      employment: {
        status: "active",
        legalEntity: "Harbor Care",
        hireDate: "2022-01-01",
        workerType: "employee",
      },
      organization: {
        legalEntity: "Harbor Care",
        businessUnit: "Healthcare",
        department: "Clinical Ops",
        team: "Nursing",
        location: "Boston",
        payZone: "US-MA",
        costCenter: "CC100",
      },
      manager: {
        employeeId: "emp_manager",
      },
      job: {
        jobCode: "RN2",
        title: "Registered Nurse",
        family: "Clinical",
        level: "L2",
      },
      compensation: {
        amount: 110000,
        currency: "USD",
        payFrequency: "annual",
        bonusTargetPercent: 10,
        effectiveDate: "2025-01-01",
      },
      emergencyContacts: [],
      custom: {
        managerChainEmployeeIds: ["emp_director"],
      },
    },
    indexedFields: {},
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  };
}

function employeeActorFixture(): ActorRecord {
  return {
    actorId: "actor_employee",
    tenantId: "tenant_demo",
    actorType: "human",
    linkedWorkerId: "emp_jane",
    displayName: "Jane Rivera",
    status: "active",
    roles: [ACTOR_ROLES.EMPLOYEE],
  };
}

function hrAdminActorFixture(): ActorRecord {
  return roleActorFixture("actor_hr", ACTOR_ROLES.HR_ADMIN);
}

function roleActorFixture(actorId: string, role: string): ActorRecord {
  return {
    actorId,
    tenantId: "tenant_demo",
    actorType: "human",
    displayName: actorId,
    status: "active",
    roles: [role],
  };
}

function approvalTaskFixture(input: {
  assigneeActorId: string;
  assigneeRole: string;
}): ApprovalTaskRecord {
  return {
    approvalTaskId: `task_${input.assigneeActorId}`,
    tenantId: "tenant_demo",
    changeRequestId: "change_request_preview",
    workflowInstanceId: "permission_preview_workflow_instance",
    assigneeActorId: input.assigneeActorId,
    assigneeRole: input.assigneeRole,
    approvalType: "preview",
    status: "pending",
    createdAt: "2026-01-01T00:00:00.000Z",
    metadata: {},
  };
}

function permissionPreviewWorkflowInstance(input: {
  requesterActorId: string;
  state: string;
}): WorkflowInstanceRecord {
  return {
    workflowInstanceId: "permission_preview_workflow_instance",
    tenantId: "tenant_demo",
    environmentId: "env_demo",
    workflowDefinitionId: "permission_preview_workflow_definition",
    workflowVersionId: "permission_preview_workflow_version",
    intent: WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    subjectType: "worker",
    subjectId: "emp_jane",
    status: "active",
    state: input.state,
    requesterActorId: input.requesterActorId,
    currentInteraction: {},
    context: {},
    startedAt: "2026-01-01T00:00:00.000Z",
    version: 1,
    correlationId: "permission_preview",
    metadata: {},
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  } as WorkflowInstanceRecord;
}

function accessContextFor(
  legacyGrants: NonNullable<EmployeeAccessEvaluationContext["legacyGrants"]>,
): EmployeeAccessEvaluationContext {
  return { legacyGrants };
}

function visibleFieldGroups(input: {
  fieldVisibility: Array<{ fieldGroup: string; allowed: boolean }>;
}): string[] {
  return input.fieldVisibility
    .filter((fieldPreview) => fieldPreview.allowed)
    .map((fieldPreview) => fieldPreview.fieldGroup);
}

function legalNameInputFixture(): Record<string, unknown> {
  return {
    newLegalName: {
      first: "Camila",
      middle: null,
      last: "Rivera",
    },
    effectiveAt: "2026-06-01",
    businessReason: "legal_name_change",
  };
}

function contactInfoInputFixture(): Record<string, unknown> {
  return {
    contact: {
      personalEmail: "jane.new@example.com",
      mobilePhone: "5550111",
      homeAddress: {
        line1: "10 Harbor",
        line2: null,
        city: "Boston",
        region: "MA",
        postalCode: "02110",
        country: "US",
      },
    },
    effectiveAt: "2026-06-01",
    businessReason: "employee_update",
  };
}

function emergencyContactInputFixture(): Record<string, unknown> {
  return {
    emergencyContact: {
      contactId: "contact_spouse",
      name: "Taylor Rivera",
      relationship: "spouse",
      phone: "5550121",
      email: "taylor@example.com",
      priority: 1,
    },
    effectiveAt: "2026-06-01",
    businessReason: "employee_update",
  };
}

function compensationInputFixture(): Record<string, unknown> {
  return {
    proposedCompensation: {
      amount: 125000,
      currency: "USD",
      payFrequency: "annual",
      bonusTargetPercent: 15,
      effectiveDate: "2026-06-01",
    },
    effectiveAt: "2026-06-01",
    businessReason: "market_adjustment",
  };
}

function orgTransferInputFixture(): Record<string, unknown> {
  return {
    targetLocationOrgUnitId: "org_location_boston",
    targetTeamOrgUnitId: "org_team_clinical_ops",
    targetCostCenterOrgUnitId: "org_cost_center_200",
    targetManagerEmployeeId: "emp_destination_manager",
    proposedJob: {
      jobCode: "RN3",
      title: "Senior Registered Nurse",
      family: "Clinical",
      level: "L3",
    },
    proposedCompensation: {
      amount: 132000,
      currency: "USD",
      payFrequency: "annual",
      bonusTargetPercent: 15,
      effectiveDate: "2026-06-01",
    },
    effectiveAt: "2026-06-01",
    businessReason: "internal_transfer",
    transferReason: "clinic_staffing_need",
    accessImpactAcknowledged: true,
  };
}

function headcountInputFixture(): Record<string, unknown> {
  return {
    department: "Clinical Ops",
    team: "Nursing",
    location: "Boston",
    costCenter: "CC100",
    jobCode: "RN2",
    title: "Registered Nurse",
    level: "L2",
    requestedFte: 1,
    targetStartDate: "2026-07-01",
    salaryRangeMin: 90000,
    salaryRangeMax: 125000,
    businessJustification: "Backfill approved clinical capacity.",
    selectedLeadershipApprovers: ["actor_leader"],
  };
}

function gateWithResolvers(
  resolvers: WorkflowApprovalGateConfig["approverResolvers"],
): WorkflowApprovalGateConfig {
  return {
    gateId: "relationship_scope_gate",
    mode: "parallel",
    interaction: "relationshipApproval",
    snapshotResolvedApprovers: true,
    taskVersionRequired: true,
    approverResolvers: resolvers,
    passRule: { type: "all_required" },
    failurePolicies: [{ type: "stop_workflow" }],
    events: {
      opened: LEDGER_EVENT_TYPES.APPROVAL_GATE_OPENED,
      taskCreated: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CREATED,
      taskDecided: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_DECIDED,
      passed: LEDGER_EVENT_TYPES.APPROVAL_GATE_PASSED,
      failed: LEDGER_EVENT_TYPES.APPROVAL_GATE_FAILED,
    },
  };
}

function validCompensationIntegrationBindings(): {
  bindings: TenantConnectorBinding[];
  targetEnvironment: "production";
  availableSecretRefs: string[];
} {
  return {
    bindings: [
      {
        abstractConnectionId: "third_party_compensation_decision",
        connectorId: "connector_comp_decision",
        environment: "production",
        secretRef: "secret_comp_decision",
        enabled: true,
        allowedOperations: ["submitCompensationChange"],
        timeoutMs: 5000,
        retryPolicy: {
          maxAttempts: 3,
          backoff: "exponential",
        },
        reconciliation: {
          required: true,
          expectedStatusPath: "status",
        },
        idempotencyScope: "node",
      },
    ],
    targetEnvironment: "production",
    availableSecretRefs: ["secret_comp_decision"],
  };
}

function validHeadcountIntegrationBindings(): {
  bindings: TenantConnectorBinding[];
  targetEnvironment: "production";
  availableSecretRefs: string[];
} {
  return {
    bindings: [
      {
        abstractConnectionId: "hris",
        connectorId: "connector_hris",
        environment: "production",
        secretRef: "secret_hris",
        enabled: true,
        allowedOperations: ["createPosition"],
        timeoutMs: 5000,
        retryPolicy: {
          maxAttempts: 3,
          backoff: "exponential",
        },
        reconciliation: {
          required: true,
          expectedStatusPath: "status",
        },
        idempotencyScope: "node",
      },
    ],
    targetEnvironment: "production",
    availableSecretRefs: ["secret_hris"],
  };
}

function unwrapPreview(
  result: ReturnType<typeof previewWorkflowPermissions>,
): ResultValue<ReturnType<typeof previewWorkflowPermissions>> {
  return unwrapResult(result, "permission preview");
}

function unwrapSimulation(
  result: ReturnType<typeof simulateWorkflow>,
): ResultValue<ReturnType<typeof simulateWorkflow>> {
  return unwrapResult(result, "simulation");
}

function unwrapDiff(
  result: ReturnType<typeof diffWorkflowConfigs>,
): ResultValue<ReturnType<typeof diffWorkflowConfigs>> {
  return unwrapResult(result, "diff");
}

function unwrapApprovalGateValidation(
  result: ReturnType<typeof validateApprovalGateAdminConfig>,
): ResultValue<ReturnType<typeof validateApprovalGateAdminConfig>> {
  return unwrapResult(result, "gate validation");
}

function unwrapResult<TValue>(
  result: { ok: true; value: TValue } | { ok: false; error: { safeMessage: string } },
  label: string,
): TValue {
  if (!result.ok) {
    throw new Error(`Expected ${label}: ${result.error.safeMessage}`);
  }

  return result.value;
}

function blockReferencesForConfig(
  workflowConfig: WorkflowConfig,
): Array<{ name: string; version: string }> {
  const blockReferences = [
    workflowConfig.submit.preflightBlock,
    workflowConfig.plan.block,
    ...Object.values(workflowConfig.metadata?.deterministicBlockRefs ?? {}),
    ...(workflowConfig.graph?.nodes ?? []).flatMap((node: WorkflowGraphNodeConfig) =>
      node.block === undefined ? [] : [node.block],
    ),
  ];

  return blockReferences.map((blockReference) => {
    return {
      name: blockReference.name,
      version: blockReference.version,
    };
  });
}

function cloneWorkflowConfig(workflowConfig: WorkflowConfig): WorkflowConfig {
  return JSON.parse(JSON.stringify(workflowConfig)) as WorkflowConfig;
}
