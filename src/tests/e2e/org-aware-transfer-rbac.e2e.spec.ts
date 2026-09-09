import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  LEDGER_EVENT_TYPES,
  WORKFLOW_INTENTS,
  WORKFLOW_TRANSITIONS,
  ok,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
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
} from "@human-capital-management-suite/data-store";
import {
  DEMO_ORGANIZATION,
  DEMO_ORG_TRANSFER_FIXTURE_ALIASES,
} from "@human-capital-management-suite/data-store/demo-organization";
import type { AppDependencies } from "../../api/dependencies.js";
import type {
  ExecutorClient,
  ExecutorRequest,
  ExecutorResponse,
} from "../../api/executor-client.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import {
  createWorkflowApiClient,
  type WorkflowApiClient,
} from "../support/workflow-api-client.js";
import orgTransferWorkflowConfigJson from "../../workflows/configs/employee-org-transfer-compensation-change.workflow.json";

const fixture = DEMO_ORG_TRANSFER_FIXTURE_ALIASES;

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  workflowApi: WorkflowApiClient;
  hrContext: ApiRequestContext;
  sourceManagerContext: ApiRequestContext;
  destinationManagerContext: ApiRequestContext;
  financeContext: ApiRequestContext;
  compensationContext: ApiRequestContext;
  medicalDirectorContext: ApiRequestContext;
  systemContext: ApiRequestContext;
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

  beforeEach(async () => {
    harness = await createHarness();
  });

  afterEach(async () => {
    await harness.workflowApi.close();
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

  it("asserts pre-workflow RBAC visibility for the transfer actors", async () => {
    const sourceManagerEmployees = await listVisibleEmployees(
      harness,
      harness.sourceManagerContext,
    );
    const destinationManagerEmployees = await listVisibleEmployees(
      harness,
      harness.destinationManagerContext,
    );

    expect(employeeIds(sourceManagerEmployees)).toContain(fixture.sourceEmployeeId);
    expect(employeeIds(destinationManagerEmployees)).not.toContain(
      fixture.sourceEmployeeId,
    );

    const sourceManagerJane = await readEmployeeProjection(
      harness,
      harness.sourceManagerContext,
      fixture.sourceEmployeeId,
    );
    const financeJane = await readEmployeeProjection(
      harness,
      harness.financeContext,
      fixture.sourceEmployeeId,
    );
    const compensationJane = await readEmployeeProjection(
      harness,
      harness.compensationContext,
      fixture.sourceEmployeeId,
    );
    const employeeJane = await readEmployeeProjection(
      harness,
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

  it("runs the full org transfer approval chain and proves post-execution RBAC", async () => {
    const transferInput = orgTransferInputFixture(harness.repositories);
    const startedWorkflow = await harness.workflowApi.startWorkflowIntent(
      harness.hrContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_ORG_TRANSFER_COMPENSATION_CHANGE,
        subjectType: "worker",
        subjectId: fixture.sourceEmployeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }

    const workflowInstanceId = String(startedWorkflow.value["workflowInstanceId"]);
    expect(startedWorkflow.value["state"]).toBe("collecting_input");

    const submittedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_org_transfer_submit",
        expectedVersion: 1,
        input: transferInput,
      },
    );

    expect(submittedWorkflow.ok).toBe(true);
    expect(submittedWorkflow.ok && submittedWorkflow.value["state"]).toBe(
      "waiting_source_manager_approval",
    );

    const sourceApprovalTask = await pendingTaskFor(
      harness,
      harness.sourceManagerContext,
    );
    const employeeActions = await availableActionTransitions(
      harness,
      harness.employeeContext,
      workflowInstanceId,
    );
    const sourceManagerActions = await availableActionTransitions(
      harness,
      harness.sourceManagerContext,
      workflowInstanceId,
    );

    expect(employeeActions).not.toContain(WORKFLOW_TRANSITIONS.APPROVE);
    expect(sourceManagerActions).toContain(WORKFLOW_TRANSITIONS.APPROVE);

    const wrongManagerAttempt = await harness.workflowApi.transitionWorkflow(
      harness.destinationManagerContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_org_transfer_wrong_manager",
        expectedVersion: 2,
        input: {
          approvalTaskId: String(sourceApprovalTask["approvalTaskId"]),
          comment: "Attempted from the wrong manager.",
        },
      },
    );
    expect(wrongManagerAttempt.ok).toBe(false);

    await approveStage({
      harness,
      context: harness.sourceManagerContext,
      workflowInstanceId,
      expectedVersion: 2,
      idempotencyKey: "idem_org_transfer_source_approve",
      expectedState: "waiting_destination_manager_approval",
    });
    await approveStage({
      harness,
      context: harness.destinationManagerContext,
      workflowInstanceId,
      expectedVersion: 3,
      idempotencyKey: "idem_org_transfer_destination_approve",
      expectedState: "waiting_finance_approval",
    });

    expect(
      await availableActionTransitions(
        harness,
        harness.compensationContext,
        workflowInstanceId,
      ),
    ).not.toContain(WORKFLOW_TRANSITIONS.APPROVE);
    expect(
      await availableActionTransitions(
        harness,
        harness.financeContext,
        workflowInstanceId,
      ),
    ).toContain(WORKFLOW_TRANSITIONS.APPROVE);

    await approveStage({
      harness,
      context: harness.financeContext,
      workflowInstanceId,
      expectedVersion: 4,
      idempotencyKey: "idem_org_transfer_finance_approve",
      expectedState: "waiting_compensation_approval",
    });
    await approveStage({
      harness,
      context: harness.compensationContext,
      workflowInstanceId,
      expectedVersion: 5,
      idempotencyKey: "idem_org_transfer_comp_approve",
      expectedState: "waiting_medical_director_approval",
    });
    await approveStage({
      harness,
      context: harness.medicalDirectorContext,
      workflowInstanceId,
      expectedVersion: 6,
      idempotencyKey: "idem_org_transfer_medical_approve",
      expectedState: "approved",
    });

    const executedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.systemContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_org_transfer_execute",
        expectedVersion: 7,
        input: {},
      },
    );

    expect(executedWorkflow.ok).toBe(true);
    const executedWorkflowValue = unwrapResult(executedWorkflow);
    expect(executedWorkflowValue["state"]).toBe("executed");

    const assignmentCountAfterExecute =
      harness.repositories.store.workerAssignments.size;
    const outboxCountAfterExecute = harness.repositories.store.integrationOutbox.size;
    const executedReplay = await harness.workflowApi.transitionWorkflow(
      harness.systemContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_org_transfer_execute",
        expectedVersion: 7,
        input: {},
      },
    );
    expect(executedReplay.ok).toBe(true);
    expect(executedReplay.ok && executedReplay.value["idempotentReplay"]).toBe(true);
    expect(harness.repositories.store.workerAssignments.size).toBe(
      assignmentCountAfterExecute,
    );
    expect(harness.repositories.store.integrationOutbox.size).toBe(
      outboxCountAfterExecute,
    );

    const updatedProjection = unwrapResult(
      harness.repositories.employeeProjections.findByEmployeeId(
        DEMO_IDS.tenantId,
        fixture.sourceEmployeeId,
      ),
    );
    expect(updatedProjection.document.organization).toMatchObject({
      location: fixture.targetLocationName,
      team: fixture.targetTeamName,
      costCenter: fixture.targetCostCenterName,
    });
    expect(updatedProjection.document.manager.employeeId).toBe(
      fixture.targetManagerEmployeeId,
    );
    expect(updatedProjection.document.compensation).toMatchObject(
      fixture.proposedCompensation,
    );

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
    ).toBe(fixture.targetTeamName);
    expect(
      orgUnitForAssignment(harness.repositories, assignmentsByType.get("work_location"))
        ?.name,
    ).toBe(fixture.targetLocationName);
    expect(
      orgUnitForAssignment(harness.repositories, assignmentsByType.get("cost_center"))
        ?.name,
    ).toBe(fixture.targetCostCenterName);

    const workflowAssignments = unwrapResult(
      harness.repositories.workerAssignments.findBySourceWorkflow(
        DEMO_IDS.tenantId,
        workflowInstanceId,
      ),
    );
    expect(
      workflowAssignments.filter((assignment) => assignment.status === "active"),
    ).toHaveLength(3);
    expect(
      workflowAssignments.filter((assignment) => assignment.status === "superseded"),
    ).toHaveLength(3);

    expect(
      (
        await readEmployeeProjectionResult(
          harness,
          harness.sourceManagerContext,
          fixture.sourceEmployeeId,
        )
      ).ok,
    ).toBe(false);
    expect(
      (
        await readEmployeeProjection(
          harness,
          harness.destinationManagerContext,
          fixture.sourceEmployeeId,
        )
      ).organization.team,
    ).toBe(fixture.targetTeamName);
    expect(
      (
        await readEmployeeProjection(
          harness,
          harness.employeeContext,
          fixture.sourceEmployeeId,
        )
      ).employeeId,
    ).toBe(fixture.sourceEmployeeId);

    const outboxRows = [
      ...harness.repositories.store.integrationOutbox.values(),
    ].filter((row) => row.changeRequestId === executedWorkflowValue["changeRequestId"]);
    expect(outboxRows.map((row) => row.destination).sort()).toEqual([
      "compensation_vendor",
      "hris",
      "payroll",
    ]);

    const timeline = unwrapResult(
      await harness.workflowApi.getTimeline(harness.hrContext, workflowInstanceId),
    );
    const eventTypes = (timeline["events"] as Array<Record<string, unknown>>).map(
      (event) => event["eventType"],
    );
    expect(eventTypes).toEqual(
      expect.arrayContaining([
        "SourceManagerApprovalCreated",
        "DestinationManagerApprovalCreated",
        "FinanceApprovalCreated",
        "CompensationApprovalCreated",
        "ClinicalPlacementApprovalCreated",
        "WorkerAssignmentSuperseded",
        "WorkerAssignmentCreated",
        "RoleBindingsRecalculated",
        "EmployeeOrgProjectionUpdated",
        "EmployeeCompensationUpdated",
        "OrgTransferExecuted",
        "WorkflowCompleted",
      ]),
    );
  });
});

async function createHarness(): Promise<TestHarness> {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const dependencies: AppDependencies = {
    repositories,
    executorClient: createOrgTransferExecutorClient(),
  };

  return {
    dependencies,
    repositories,
    workflowApi: await createWorkflowApiClient(dependencies),
    hrContext: createRequestContext(mustActor(repositories, DEMO_IDS.hrActorId)),
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
    medicalDirectorContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.medicalDirectorActorId),
    ),
    systemContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.systemActorId),
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

function createOrgTransferExecutorClient(): ExecutorClient {
  return {
    async executeBlock<TOutput>(
      request: ExecutorRequest,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      const output = request.block.name.endsWith(".preflight")
        ? {
            valid: true,
            riskLevel: "medium",
            requiresEvidence: false,
            requiresApproval: true,
            warnings: [],
            errors: [],
          }
        : buildOrgTransferPlanOutput(request);

      return ok({
        status: "succeeded",
        output: output as TOutput,
        proposedEvents: [],
        externalCallRequests: [],
        logs: [],
        metrics: {},
      });
    },
  };
}

function buildOrgTransferPlanOutput(request: ExecutorRequest): Record<string, unknown> {
  const effectiveAt = stringValue(request.input["effectiveAt"]);
  const workerId = stringValue(request.input["workerId"]);
  const currentAssignments = recordArray(request.input["currentAssignments"]);
  const proposedTransfer = recordValue(request.input["proposedTransfer"]);
  const proposedOrganization =
    Object.keys(recordValue(request.input["proposedOrganization"])).length > 0
      ? recordValue(request.input["proposedOrganization"])
      : proposedOrganizationFixture();
  const proposedJob =
    Object.keys(recordValue(request.input["proposedJob"])).length > 0
      ? recordValue(request.input["proposedJob"])
      : recordValue(proposedTransfer["proposedJob"]);
  const proposedCompensation =
    Object.keys(recordValue(request.input["proposedCompensation"])).length > 0
      ? recordValue(request.input["proposedCompensation"])
      : recordValue(proposedTransfer["proposedCompensation"]);
  const targetTeam =
    Object.keys(recordValue(request.input["targetTeamOrgUnit"])).length > 0
      ? recordValue(request.input["targetTeamOrgUnit"])
      : orgUnitFixture(
          stringValue(proposedTransfer["targetTeamOrgUnitId"]),
          fixture.targetTeamName,
        );
  const targetLocation =
    Object.keys(recordValue(request.input["targetLocationOrgUnit"])).length > 0
      ? recordValue(request.input["targetLocationOrgUnit"])
      : orgUnitFixture(
          stringValue(proposedTransfer["targetLocationOrgUnitId"]),
          fixture.targetLocationName,
        );
  const targetCostCenter =
    Object.keys(recordValue(request.input["targetCostCenterOrgUnit"])).length > 0
      ? recordValue(request.input["targetCostCenterOrgUnit"])
      : orgUnitFixture(
          stringValue(proposedTransfer["targetCostCenterOrgUnitId"]),
          fixture.targetCostCenterName,
        );
  const targetManagerEmployeeId = firstNonEmptyString(
    request.input["targetManagerEmployeeId"],
    proposedTransfer["targetManagerEmployeeId"],
  );

  const supersededAssignmentTypes = new Set([
    "primary_team",
    "work_location",
    "cost_center",
  ]);
  const currentAssignmentsForSupersede =
    currentAssignments.length > 0
      ? currentAssignments
      : [
          { assignmentType: "primary_team" },
          { assignmentType: "work_location" },
          { assignmentType: "cost_center" },
        ];
  const supersedeOperations = currentAssignmentsForSupersede
    .filter((assignment) => {
      return supersededAssignmentTypes.has(stringValue(assignment["assignmentType"]));
    })
    .map((assignment) => {
      const assignmentType = stringValue(assignment["assignmentType"]);
      return {
        operation: "supersede",
        assignmentType,
        currentWorkerAssignmentId: stringValue(assignment["workerAssignmentId"]),
        effectiveEnd: effectiveAt,
        idempotencyKey: [
          "org_transfer",
          request.workflowInstanceId,
          assignmentType,
          "supersede",
        ].join(":"),
      };
    });

  const createOperations = [
    {
      operation: "create",
      assignmentType: "primary_team",
      orgUnitId: stringValue(targetTeam["orgUnitId"]),
      roleType: "primary",
      managerEmployeeId: targetManagerEmployeeId,
      allocationPercent: 100,
      effectiveStart: effectiveAt,
      idempotencyKey: `org_transfer:${request.workflowInstanceId}:primary_team:create`,
    },
    {
      operation: "create",
      assignmentType: "work_location",
      orgUnitId: stringValue(targetLocation["orgUnitId"]),
      roleType: "primary",
      managerEmployeeId: targetManagerEmployeeId,
      allocationPercent: 100,
      effectiveStart: effectiveAt,
      idempotencyKey: `org_transfer:${request.workflowInstanceId}:work_location:create`,
    },
    {
      operation: "create",
      assignmentType: "cost_center",
      orgUnitId: stringValue(targetCostCenter["orgUnitId"]),
      roleType: "primary",
      managerEmployeeId: targetManagerEmployeeId,
      allocationPercent: 100,
      effectiveStart: effectiveAt,
      idempotencyKey: `org_transfer:${request.workflowInstanceId}:cost_center:create`,
    },
  ];

  return {
    internalWrites: [
      ...supersedeOperations.map((operation) => ({
        eventType: LEDGER_EVENT_TYPES.WORKER_ASSIGNMENT_SUPERSEDED,
        subjectType: "worker",
        subjectId: workerId,
        effectiveAt,
        payload: { operation },
      })),
      ...createOperations.map((operation) => ({
        eventType: LEDGER_EVENT_TYPES.WORKER_ASSIGNMENT_CREATED,
        subjectType: "worker",
        subjectId: workerId,
        effectiveAt,
        payload: { operation },
      })),
      {
        eventType: LEDGER_EVENT_TYPES.ROLE_BINDINGS_RECALCULATED,
        subjectType: "worker",
        subjectId: workerId,
        effectiveAt,
        payload: {
          operations: [
            {
              operation: "ensure_direct_reports_binding",
              actorEmployeeId: targetManagerEmployeeId,
            },
            { operation: "record_recalculation" },
          ],
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.EMPLOYEE_ORG_PROJECTION_UPDATED,
        subjectType: "worker",
        subjectId: workerId,
        effectiveAt,
        payload: {
          organization: proposedOrganization,
          targetManagerEmployeeId,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.EMPLOYEE_COMPENSATION_UPDATED,
        subjectType: "worker",
        subjectId: workerId,
        effectiveAt,
        payload: {
          compensation: proposedCompensation,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.ORG_TRANSFER_EXECUTED,
        subjectType: "worker",
        subjectId: workerId,
        effectiveAt,
        payload: {
          targetManagerEmployeeId,
          proposedOrganization,
        },
      },
    ],
    projectionPatches: [
      {
        projection: "employee",
        operation: "replace",
        path: "/organization",
        value: proposedOrganization,
      },
      {
        projection: "employee",
        operation: "replace",
        path: "/manager/employeeId",
        value: targetManagerEmployeeId,
      },
      {
        projection: "employee",
        operation: "replace",
        path: "/job",
        value: proposedJob,
      },
      {
        projection: "employee",
        operation: "replace",
        path: "/compensation",
        value: proposedCompensation,
      },
    ],
    assignmentOperations: [...supersedeOperations, ...createOperations],
    roleBindingOperations: [
      {
        operation: "ensure_direct_reports_binding",
        actorEmployeeId: targetManagerEmployeeId,
        roleKey: "manager",
        scopeType: "direct_reports",
        effectiveStart: effectiveAt,
        idempotencyKey: `org_transfer:${request.workflowInstanceId}:role_binding:destination_manager`,
      },
      {
        operation: "record_recalculation",
        idempotencyKey: `org_transfer:${request.workflowInstanceId}:role_binding:recalculation`,
      },
    ],
    externalCallRequests: [
      {
        connectionId: "hris",
        operation: "syncWorkerTransfer",
        idempotencyKey: `org_transfer:${request.workflowInstanceId}:hris`,
        payload: {
          workerId,
          proposedOrganization,
          targetManagerEmployeeId,
          effectiveAt,
        },
      },
      {
        connectionId: "payroll",
        operation: "syncCostCenter",
        idempotencyKey: `org_transfer:${request.workflowInstanceId}:payroll`,
        payload: {
          workerId,
          targetCostCenter: stringValue(targetCostCenter["name"]),
          effectiveAt,
        },
      },
      {
        connectionId: "compensation_vendor",
        operation: "syncCompensation",
        idempotencyKey: `org_transfer:${request.workflowInstanceId}:compensation_vendor`,
        payload: {
          workerId,
          compensation: proposedCompensation,
          effectiveAt,
        },
      },
    ],
  };
}

function proposedOrganizationFixture(): Record<string, unknown> {
  return {
    legalEntity: "HarborCare Medical Group PC",
    businessUnit: "Clinical Operations",
    department: "Clinical Care",
    team: fixture.targetTeamName,
    location: fixture.targetLocationName,
    payZone: "US-EAST",
    costCenter: fixture.targetCostCenterName,
  };
}

function orgUnitFixture(
  orgUnitId: string | undefined,
  name: string,
): Record<string, unknown> {
  return {
    orgUnitId,
    name,
  };
}

async function listVisibleEmployees(
  harness: TestHarness,
  requestContext: ApiRequestContext,
): Promise<EmployeeProjectionRecord[]> {
  const result = await harness.workflowApi.listEmployeeProjections(requestContext);
  const value = unwrapResult(result);

  return value["employees"] as EmployeeProjectionRecord[];
}

async function readEmployeeProjection(
  harness: TestHarness,
  requestContext: ApiRequestContext,
  employeeId: string,
): Promise<EmployeeProjectionDocument> {
  const value = unwrapResult(
    await readEmployeeProjectionResult(harness, requestContext, employeeId),
  );
  const projection = value["projection"] as { document?: EmployeeProjectionDocument };

  return projection.document ?? (projection as unknown as EmployeeProjectionDocument);
}

function readEmployeeProjectionResult(
  harness: TestHarness,
  requestContext: ApiRequestContext,
  employeeId: string,
): Promise<Result<Record<string, unknown>, AppError>> {
  return harness.workflowApi.getEmployeeProjection(requestContext, employeeId);
}

async function pendingTaskFor(
  harness: TestHarness,
  requestContext: ApiRequestContext,
): Promise<Record<string, unknown>> {
  const tasks = unwrapResult(await harness.workflowApi.getTasks(requestContext));
  const task = (tasks["tasks"] as Array<Record<string, unknown>>)[0];

  if (task === undefined) {
    throw new Error(`Missing pending task for ${requestContext.actor.actorId}`);
  }

  return task;
}

async function availableActionTransitions(
  harness: TestHarness,
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
): Promise<string[]> {
  const actionsResult = await harness.workflowApi.getAvailableActions(
    requestContext,
    workflowInstanceId,
  );
  if (!actionsResult.ok) {
    return [];
  }

  const actionsPayload = unwrapResult(actionsResult);
  const actions = actionsPayload["actions"] as Array<Record<string, unknown>>;

  return actions.map((action) => String(action["transition"]));
}

async function approveStage(input: {
  harness: TestHarness;
  context: ApiRequestContext;
  workflowInstanceId: string;
  expectedVersion: number;
  idempotencyKey: string;
  expectedState: string;
}): Promise<void> {
  const task = await pendingTaskFor(input.harness, input.context);
  const approvedWorkflow = await input.harness.workflowApi.transitionWorkflow(
    input.context,
    input.workflowInstanceId,
    {
      transition: WORKFLOW_TRANSITIONS.APPROVE,
      idempotencyKey: input.idempotencyKey,
      expectedVersion: input.expectedVersion,
      input: {
        approvalTaskId: String(task["approvalTaskId"]),
        comment: `${input.expectedState} approved in e2e.`,
      },
    },
  );

  const approvedWorkflowValue = unwrapResult(approvedWorkflow);
  expect(approvedWorkflowValue["state"]).toBe(input.expectedState);
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

function stringValue(value: unknown): string {
  return firstNonEmptyString(value);
}

function firstNonEmptyString(...values: unknown[]): string {
  for (const value of values) {
    if (typeof value === "string" && value.trim().length > 0) {
      return value;
    }
  }

  return "";
}

function recordValue(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return {};
  }

  return value as Record<string, unknown>;
}

function recordArray(value: unknown): Record<string, unknown>[] {
  if (!Array.isArray(value)) {
    return [];
  }

  return value.map(recordValue);
}
