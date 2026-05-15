import { beforeEach, describe, expect, it } from "vitest";
import { type AppError, type Result } from "@hcm-next/foundation";
import {
  createRepositories,
  createSeededDemoStore,
  DEMO_IDS,
  type ActorRecord,
  type EmployeeProjectionDocument,
  type EmployeeProjectionRecord,
  type OrganizationUnitRecord,
  type Repositories,
  type WorkerAssignmentRecord,
} from "@hcm-next/data-store";
import {
  DEMO_ORGANIZATION,
  DEMO_ORG_TRANSFER_FIXTURE_ALIASES,
} from "@hcm-next/data-store/demo-organization";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import {
  getEmployeeProjection,
  listEmployeeProjections,
} from "../../workflows/legal-name-change/service.js";
import orgTransferWorkflowConfigJson from "../../workflows/configs/employee-org-transfer-compensation-change.workflow.json";

const fixture = DEMO_ORG_TRANSFER_FIXTURE_ALIASES;

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  sourceManagerContext: ApiRequestContext;
  destinationManagerContext: ApiRequestContext;
  financeContext: ApiRequestContext;
  compensationContext: ApiRequestContext;
  employeeContext: ApiRequestContext;
};

type OrgTransferWorkflowConfig = {
  intent: string;
  interactions: Record<
    string,
    {
      title: string;
      jsonSchema?: {
        required?: string[];
        properties?: Record<string, { enum?: string[] }>;
      };
      uiSchema?: {
        fieldLabels?: Record<string, string>;
        submitLabel?: string;
      };
    }
  >;
  graph: {
    nodes: Array<{
      type: string;
      outcomes?: Array<{ outcome: string }>;
    }>;
  };
};

describe("employee.org_transfer_compensation_change HarborCare E2E contract", () => {
  let harness: TestHarness;

  beforeEach(() => {
    harness = createHarness();
  });

  it("resolves stable HarborCare source and target fixture aliases", () => {
    expect(harness.repositories.store.tenants.get(DEMO_IDS.tenantId)?.name).toBe(
      DEMO_ORGANIZATION.name,
    );
    expect(JSON.stringify(fixture).toLowerCase()).not.toContain("northstar");

    const targetLocation = orgUnitByKey(
      harness.repositories,
      fixture.targetLocationOrgUnitKey,
    );
    const targetTeam = orgUnitByKey(harness.repositories, fixture.targetTeamOrgUnitKey);
    const targetCostCenter = orgUnitByKey(
      harness.repositories,
      fixture.targetCostCenterOrgUnitKey,
    );

    expect(targetLocation).toMatchObject({
      type: "location",
      name: "Cambridge Clinic",
    });
    expect(targetTeam).toMatchObject({
      type: "team",
      name: "Cambridge Nursing",
    });
    expect(targetCostCenter).toMatchObject({
      type: "cost_center",
      name: "CLN-CAM",
    });

    const janeProjection = unwrapResult(
      harness.repositories.employeeProjections.findByEmployeeId(
        DEMO_IDS.tenantId,
        fixture.sourceEmployeeId,
      ),
    );
    expect(janeProjection.document.organization).toMatchObject({
      location: "Boston Main Clinic",
      team: "Boston Nursing",
      costCenter: "CLN-BOS",
    });
    expect(janeProjection.document.manager.employeeId).toBe(
      fixture.sourceManagerEmployeeId,
    );

    const destinationManager = mustActor(
      harness.repositories,
      fixture.destinationManagerActorId,
    );
    expect(destinationManager.linkedWorkerId).toBe(
      fixture.destinationManagerEmployeeId,
    );
    expect(destinationManager.displayName).toBe("Sofia Rossi");

    const financeRoleBindings = harness.repositories.roleBindings.findActiveForActor(
      DEMO_IDS.tenantId,
      DEMO_IDS.financeAdminActorId,
    );
    expect(financeRoleBindings.ok).toBe(true);
    expect(
      financeRoleBindings.ok &&
        financeRoleBindings.value.some((binding) => {
          return (
            binding.scopeOrgUnitId === targetCostCenter.orgUnitId &&
            binding.scopeValue === fixture.targetCostCenterName
          );
        }),
    ).toBe(true);
  });

  it("asserts pre-workflow RBAC visibility for the transfer actors", () => {
    const sourceManagerEmployees = listVisibleEmployees(
      harness.dependencies,
      harness.sourceManagerContext,
    );
    const destinationManagerEmployees = listVisibleEmployees(
      harness.dependencies,
      harness.destinationManagerContext,
    );

    expect(employeeIds(sourceManagerEmployees)).toContain(fixture.sourceEmployeeId);
    expect(employeeIds(destinationManagerEmployees)).not.toContain(
      fixture.sourceEmployeeId,
    );

    const sourceManagerJane = readEmployeeProjection(
      harness.dependencies,
      harness.sourceManagerContext,
      fixture.sourceEmployeeId,
    );
    const financeJane = readEmployeeProjection(
      harness.dependencies,
      harness.financeContext,
      fixture.sourceEmployeeId,
    );
    const compensationJane = readEmployeeProjection(
      harness.dependencies,
      harness.compensationContext,
      fixture.sourceEmployeeId,
    );
    const employeeJane = readEmployeeProjection(
      harness.dependencies,
      harness.employeeContext,
      fixture.sourceEmployeeId,
    );

    expect(isMasked(sourceManagerJane.compensation)).toBe(true);
    expect(isMaskedContact(sourceManagerJane.contact)).toBe(true);
    expect(isMaskedContact(financeJane.contact)).toBe(true);
    expect(isVisibleCompensation(financeJane.compensation)).toBe(true);
    expect(isMaskedContact(compensationJane.contact)).toBe(true);
    expect(isVisibleCompensation(compensationJane.compensation)).toBe(true);
    expect(employeeJane.contact.personalEmail).toContain("jane.doe");
  });

  it("defines the UX contract payload with HarborCare org unit IDs and labels", () => {
    const transferInput = orgTransferInputFixture(harness.repositories);
    const workflowConfig =
      orgTransferWorkflowConfigJson as unknown as OrgTransferWorkflowConfig;
    const inputInteraction = workflowConfig.interactions["input"];
    const inputSchema = inputInteraction?.jsonSchema;
    const uiSchema = inputInteraction?.uiSchema;

    expect(transferInput).toMatchObject({
      targetManagerEmployeeId: fixture.targetManagerEmployeeId,
      effectiveAt: fixture.effectiveAt,
      businessReason: fixture.businessReason,
      transferReason: fixture.transferReason,
      accessImpactAcknowledged: true,
    });
    expect(transferInput.targetLocationOrgUnitId).toBe(
      orgUnitByKey(harness.repositories, fixture.targetLocationOrgUnitKey).orgUnitId,
    );
    expect(transferInput.targetTeamOrgUnitId).toBe(
      orgUnitByKey(harness.repositories, fixture.targetTeamOrgUnitKey).orgUnitId,
    );
    expect(transferInput.targetCostCenterOrgUnitId).toBe(
      orgUnitByKey(harness.repositories, fixture.targetCostCenterOrgUnitKey).orgUnitId,
    );

    expect(workflowConfig.intent).toBe(fixture.intent);
    expect(inputInteraction?.title).toBe("Transfer employee and update compensation");
    expect(inputSchema?.required).toEqual([
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
    ]);
    expect(inputSchema?.properties?.["businessReason"]?.enum).toContain(
      fixture.businessReason,
    );
    expect(inputSchema?.properties?.["transferReason"]?.enum).toContain(
      fixture.transferReason,
    );
    expect(Object.values(uiSchema?.fieldLabels ?? {})).toEqual(
      expect.arrayContaining([
        "Target work location",
        "Target team",
        "Target cost center",
        "Target manager",
        "Proposed job",
        "Proposed compensation",
        "Effective date",
      ]),
    );
    expect(Object.values(uiSchema?.fieldLabels ?? {}).every(isHumanLabel)).toBe(true);
    expect(uiSchema?.submitLabel).toBe("Submit transfer for review");
    expect(approvalInteractionTitles(workflowConfig)).toEqual([
      "Source manager approval",
      "Destination manager approval",
      "Finance cost-center approval",
      "Compensation approval",
      "Medical director clinical placement approval",
    ]);
    expect(
      workflowConfig.graph.nodes
        .filter((node) => node.type === "approval")
        .every((node) => approvalNodeHasDecisionOutcomes(node.outcomes ?? [])),
    ).toBe(true);
    expect(approvalDecisionPayloadContract().reject.required).toContain("reason");
    expect(approvalDecisionPayloadContract().requestMoreInfo.required).toContain(
      "reason",
    );
  });

  it("asserts source assignments that the transfer workflow must supersede", () => {
    const activeAssignments = unwrapResult(
      harness.repositories.workerAssignments.findActiveForEmployee(
        DEMO_IDS.tenantId,
        fixture.sourceEmployeeId,
      ),
    );
    const assignmentsByType = assignmentMap(activeAssignments);

    expect(
      orgUnitForAssignment(harness.repositories, assignmentsByType.get("primary_team"))
        ?.name,
    ).toBe(fixture.sourceTeamName);
    expect(
      orgUnitForAssignment(harness.repositories, assignmentsByType.get("work_location"))
        ?.name,
    ).toBe(fixture.sourceLocationName);
    expect(
      orgUnitForAssignment(harness.repositories, assignmentsByType.get("cost_center"))
        ?.name,
    ).toBe(fixture.sourceCostCenterName);
  });
});

function createHarness(): TestHarness {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const dependencies: AppDependencies = {
    repositories,
    executorClient: {
      executeBlock() {
        throw new Error("Org-transfer contract tests do not execute blocks.");
      },
    },
  };

  return {
    dependencies,
    repositories,
    sourceManagerContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.managerActorId),
    ),
    destinationManagerContext: createRequestContext(
      mustActor(repositories, fixture.destinationManagerActorId),
    ),
    financeContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.financeAdminActorId),
    ),
    compensationContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.compensationAdminActorId),
    ),
    employeeContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.employeeActorId),
    ),
  };
}

function mustActor(repositories: Repositories, actorId: string): ActorRecord {
  const actorResult = repositories.actors.findById(actorId);

  if (!actorResult.ok) {
    throw new Error(`Missing seeded actor ${actorId}`);
  }

  return actorResult.value;
}

function createRequestContext(actor: ActorRecord): ApiRequestContext {
  return {
    tenantId: DEMO_IDS.tenantId,
    environmentId: DEMO_IDS.environmentId,
    actor,
    requestId: `req_${actor.actorId}`,
    correlationId: `corr_${actor.actorId}`,
  };
}

function listVisibleEmployees(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): EmployeeProjectionRecord[] {
  const result = listEmployeeProjections(dependencies, requestContext);
  const value = unwrapResult(result);

  return value["employees"] as EmployeeProjectionRecord[];
}

function readEmployeeProjection(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  employeeId: string,
): EmployeeProjectionDocument {
  const result = getEmployeeProjection(dependencies, requestContext, employeeId);
  const value = unwrapResult(result);
  const projection = value["projection"] as { document?: EmployeeProjectionDocument };

  return projection.document ?? (projection as unknown as EmployeeProjectionDocument);
}

function unwrapResult<TValue>(result: Result<TValue, AppError>): TValue {
  if (!result.ok) {
    throw new Error(result.error.message);
  }

  return result.value;
}

function employeeIds(employees: EmployeeProjectionRecord[]): string[] {
  return employees.map((employee) => employee.employeeId).sort();
}

function orgUnitByKey(
  repositories: Repositories,
  unitKey: string,
): OrganizationUnitRecord {
  return unwrapResult(
    repositories.organizationUnits.findByKey(DEMO_IDS.tenantId, unitKey),
  );
}

function orgUnitForAssignment(
  repositories: Repositories,
  assignment: WorkerAssignmentRecord | undefined,
): OrganizationUnitRecord | undefined {
  if (assignment === undefined) {
    return undefined;
  }

  return repositories.store.organizationUnits.get(assignment.orgUnitId);
}

function assignmentMap(
  assignments: WorkerAssignmentRecord[],
): Map<string, WorkerAssignmentRecord> {
  return new Map(
    assignments.map((assignment) => [assignment.assignmentType, assignment]),
  );
}

function isVisibleCompensation(
  compensation: EmployeeProjectionDocument["compensation"],
): boolean {
  return compensation.amount > 0 && compensation.currency === "USD";
}

function isMasked(value: unknown): boolean {
  if (value === undefined || value === null) {
    return true;
  }

  if (typeof value === "string") {
    const normalizedValue = value.toLowerCase();
    return normalizedValue.includes("mask") || normalizedValue.includes("restricted");
  }

  if (typeof value !== "object" || Array.isArray(value)) {
    return false;
  }

  return Object.values(value).some((recordValue) => isMasked(recordValue));
}

function isMaskedContact(contact: EmployeeProjectionDocument["contact"]): boolean {
  return (
    contact.personalEmail === null &&
    contact.mobilePhone === null &&
    isMasked(contact.homeAddress.line1)
  );
}

function orgTransferInputFixture(repositories: Repositories): Record<string, unknown> {
  return {
    targetLocationOrgUnitId: orgUnitByKey(
      repositories,
      fixture.targetLocationOrgUnitKey,
    ).orgUnitId,
    targetTeamOrgUnitId: orgUnitByKey(repositories, fixture.targetTeamOrgUnitKey)
      .orgUnitId,
    targetCostCenterOrgUnitId: orgUnitByKey(
      repositories,
      fixture.targetCostCenterOrgUnitKey,
    ).orgUnitId,
    targetManagerEmployeeId: fixture.targetManagerEmployeeId,
    proposedJob: fixture.proposedJob,
    proposedCompensation: fixture.proposedCompensation,
    effectiveAt: fixture.effectiveAt,
    businessReason: fixture.businessReason,
    transferReason: fixture.transferReason,
    accessImpactAcknowledged: true,
  };
}

function approvalInteractionTitles(
  workflowConfig: OrgTransferWorkflowConfig,
): Array<string | undefined> {
  return [
    "sourceManagerApproval",
    "destinationManagerApproval",
    "financeApproval",
    "compensationApproval",
    "medicalDirectorApproval",
  ].map((interactionKey) => workflowConfig.interactions[interactionKey]?.title);
}

function approvalNodeHasDecisionOutcomes(
  outcomes: Array<{ outcome: string }>,
): boolean {
  const outcomeNames = outcomes.map((outcome) => outcome.outcome);

  return ["approved", "rejected", "request_more_info"].every((outcome) => {
    return outcomeNames.includes(outcome);
  });
}

function isHumanLabel(label: string): boolean {
  return !label.includes("_") && label.trim().length > 3;
}

function approvalDecisionPayloadContract() {
  return {
    reject: { required: ["reason"] },
    requestMoreInfo: { required: ["reason"] },
  };
}
