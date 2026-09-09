import { readFileSync } from "node:fs";
import { resolve } from "node:path";
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
  type ApprovalTaskRecord,
  type Repositories,
} from "@human-capital-management-suite/data-store";
import {
  DEMO_ORGANIZATION,
  orgUnitKeyForCostCenter,
  orgUnitKeyForLocation,
  orgUnitKeyForTeam,
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

const configPath = resolve(
  process.cwd(),
  "src/workflows/configs/position-headcount-requisition.workflow.json",
);

const headcountFixture = {
  intent: WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL,
  subjectType: "position",
  subjectId: "position_req_senior_rn_cambridge_nursing",
  requesterActorId: DEMO_IDS.hrActorId,
  leadershipApprover1ActorId: DEMO_IDS.managerActorId,
  leadershipApprover2ActorId: DEMO_IDS.destinationManagerActorId,
  financeApproverActorId: DEMO_IDS.financeAdminActorId,
  hrbpApproverActorId: DEMO_IDS.secondAdminActorId,
  compensationApproverActorId: DEMO_IDS.compensationAdminActorId,
  medicalDirectorApproverActorId: DEMO_IDS.medicalDirectorActorId,
  clinicOpsApproverActorId: DEMO_IDS.clinicOpsActorId,
  systemActorId: DEMO_IDS.systemActorId,
  leadershipGateId: "leadership_chain_gate",
  asyncGateId: "cross_functional_gate",
  request: {
    department: "Clinical Care",
    team: "Cambridge Nursing",
    location: "Cambridge Clinic",
    costCenter: "CLN-CAM",
    jobCode: "CLN-RN3",
    title: "Senior Registered Nurse",
    level: "P3",
    requestedFte: 1,
    targetStartDate: "2026-07-01",
    salaryRangeMin: 98000,
    salaryRangeMax: 116000,
    businessJustification:
      "Cambridge Nursing needs one Senior RN to cover expanded evening triage volume.",
  },
} as const;

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  workflowApi: WorkflowApiClient;
  requesterContext: ApiRequestContext;
  leadershipApprover1Context: ApiRequestContext;
  leadershipApprover2Context: ApiRequestContext;
  financeContext: ApiRequestContext;
  hrbpContext: ApiRequestContext;
  compensationContext: ApiRequestContext;
  medicalDirectorContext: ApiRequestContext;
  clinicOpsContext: ApiRequestContext;
  systemContext: ApiRequestContext;
};

type WorkflowService = {
  startWorkflowIntent(
    dependencies: AppDependencies,
    requestContext: ApiRequestContext,
    body: Record<string, unknown>,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  transitionWorkflow(
    dependencies: AppDependencies,
    requestContext: ApiRequestContext,
    workflowInstanceId: string,
    body: Record<string, unknown>,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  getTasks(
    dependencies: AppDependencies,
    requestContext: ApiRequestContext,
  ): Result<Record<string, unknown>, AppError>;
  getTimeline(
    dependencies: AppDependencies,
    requestContext: ApiRequestContext,
    workflowInstanceId: string,
    view?: string,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  getAvailableActions(
    dependencies: AppDependencies,
    requestContext: ApiRequestContext,
    workflowInstanceId: string,
  ): Promise<Result<Record<string, unknown>, AppError>>;
};

type WorkflowConfigContract = {
  intent: string;
  subjectType: string;
  interactions: Record<
    string,
    {
      title: string;
      jsonSchema?: {
        required?: string[];
        properties?: Record<string, Record<string, unknown>>;
      };
      uiSchema?: {
        fieldLabels?: Record<string, string>;
        submitLabel?: string;
      };
    }
  >;
  graph?: {
    nodes: Array<{
      nodeId: string;
      type: string;
      title: string;
      approvalGate?: {
        gateId: string;
        mode: string;
        taskVersionRequired: boolean;
        approverResolvers: Array<{
          resolverId: string;
          label: string;
          fieldPath?: string;
          role?: string;
          isVetoHolder?: boolean;
        }>;
        passRule: Record<string, unknown>;
        events: Record<string, string>;
      };
    }>;
  };
};

describe("position.headcount_requisition.approval HarborCare fixtures", () => {
  it("resolves stable HarborCare actor aliases and Cambridge Nursing request data", async () => {
    const harness = await createHarness();

    expect(harness.repositories.store.tenants.get(DEMO_IDS.tenantId)?.name).toBe(
      DEMO_ORGANIZATION.name,
    );
    expect(
      mustActor(harness.repositories, headcountFixture.requesterActorId),
    ).toMatchObject({
      displayName: "Grace Kim",
    });
    expect(
      mustActor(harness.repositories, headcountFixture.leadershipApprover1ActorId),
    ).toMatchObject({
      displayName: "Morgan Lee",
    });
    expect(
      mustActor(harness.repositories, headcountFixture.leadershipApprover2ActorId),
    ).toMatchObject({
      displayName: "Sofia Rossi",
    });
    expect(
      mustActor(harness.repositories, headcountFixture.financeApproverActorId)
        .displayName,
    ).toBe("Avery Chen");
    expect(
      mustActor(harness.repositories, headcountFixture.hrbpApproverActorId).displayName,
    ).toBe("Riley Patel");
    expect(
      mustActor(harness.repositories, headcountFixture.compensationApproverActorId)
        .displayName,
    ).toBe("Jordan Rivera");
    expect(
      mustActor(harness.repositories, headcountFixture.medicalDirectorApproverActorId)
        .displayName,
    ).toBe("Daniel Cho");
    expect(
      mustActor(harness.repositories, headcountFixture.clinicOpsApproverActorId)
        .displayName,
    ).toBe("Priya Nair");

    expect(
      orgUnitIdByKey(harness.repositories, orgUnitKeyForLocation("Cambridge Clinic")),
    ).toBe("orgunit_location_cambridge_clinic");
    expect(
      orgUnitIdByKey(
        harness.repositories,
        orgUnitKeyForTeam("Clinical Operations", "Clinical Care", "Cambridge Nursing"),
      ),
    ).toBe("orgunit_team_clinical_operations_clinical_care_cambridge_nursing");
    expect(
      orgUnitIdByKey(harness.repositories, orgUnitKeyForCostCenter("CLN-CAM")),
    ).toBe("orgunit_cost_center_cln_cam");

    expect(headcountInputFixture()).toMatchObject({
      department: "Clinical Care",
      team: "Cambridge Nursing",
      location: "Cambridge Clinic",
      costCenter: "CLN-CAM",
      jobCode: "CLN-RN3",
      title: "Senior Registered Nurse",
      level: "P3",
      requestedFte: 1,
      selectedLeadershipApprovers: [
        headcountFixture.leadershipApprover1ActorId,
        headcountFixture.leadershipApprover2ActorId,
      ],
    });

    await harness.workflowApi.close();
  });
});

describe("position.headcount_requisition.approval UX contract", () => {
  it("defines intake labels, required schema fields, gates, quorum, and summaries", () => {
    const workflowConfig = readHeadcountWorkflowConfig();
    const inputInteraction = workflowConfig.interactions["input"];
    const inputSchema = inputInteraction?.jsonSchema;
    const fieldLabels = inputInteraction?.uiSchema?.fieldLabels ?? {};
    const gateNodes = workflowConfig.graph?.nodes.filter((node) => {
      return node.type === "approval_gate";
    });
    const leadershipGate = gateNodes?.find((node) => {
      return node.approvalGate?.gateId === headcountFixture.leadershipGateId;
    });
    const asyncGate = gateNodes?.find((node) => {
      return node.approvalGate?.gateId === headcountFixture.asyncGateId;
    });

    expect(workflowConfig.intent).toBe(headcountFixture.intent);
    expect(workflowConfig.subjectType).toBe(headcountFixture.subjectType);
    expect(inputInteraction?.title).toMatch(/headcount/i);
    expect(inputSchema?.required).toEqual(
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
    expect(Object.values(fieldLabels)).toEqual(
      expect.arrayContaining([
        "Department",
        "Team",
        "Location",
        "Cost center",
        "Job code",
        "Title",
        "Level",
        "Requested FTE",
        "Target start date",
        "Salary range minimum",
        "Salary range maximum",
        "Business justification",
        "Leadership approval chain",
      ]),
    );
    expect(Object.values(fieldLabels).every(isHumanLabel)).toBe(true);
    expect(inputInteraction?.uiSchema?.submitLabel).toMatch(/submit/i);

    expect(leadershipGate?.approvalGate).toMatchObject({
      mode: "sequential",
      taskVersionRequired: true,
    });
    expect(leadershipGate?.approvalGate?.approverResolvers).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          fieldPath: expect.stringContaining("selectedLeadershipApprovers"),
        }),
      ]),
    );
    expect(asyncGate?.approvalGate).toMatchObject({
      mode: "parallel",
      taskVersionRequired: true,
      passRule: {
        type: "quorum",
        requiredApprovals: 3,
        eligibleApprovals: 5,
      },
    });
    expect(asyncGate?.approvalGate?.approverResolvers).toHaveLength(5);
    expect(asyncGate?.approvalGate?.approverResolvers).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          resolverId: expect.stringContaining("finance"),
          isVetoHolder: true,
        }),
        expect.objectContaining({
          resolverId: expect.stringContaining("medical_director"),
          isVetoHolder: true,
        }),
        expect.objectContaining({ resolverId: expect.stringContaining("hrbp") }),
        expect.objectContaining({
          resolverId: expect.stringContaining("compensation"),
        }),
        expect.objectContaining({
          resolverId: expect.stringContaining("clinic_ops"),
        }),
      ]),
    );
    expect(asyncGate?.approvalGate?.events).toMatchObject({
      opened: LEDGER_EVENT_TYPES.APPROVAL_GATE_OPENED,
      taskCreated: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CREATED,
      taskDecided: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_DECIDED,
      passed: LEDGER_EVENT_TYPES.APPROVAL_GATE_PASSED,
      failed: LEDGER_EVENT_TYPES.APPROVAL_GATE_FAILED,
      taskCanceled: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CANCELED,
    });
    expect(
      stringRecord(workflowConfig.interactions["crossFunctionalApproval"]?.uiSchema)[
        "quorumLabel"
      ],
    ).toMatch(/3.*5|quorum/i);
    expect(workflowConfig.interactions["repair"]?.title).toMatch(
      /repair|revise|more information/i,
    );
    expect(workflowConfig.interactions["completedSummary"]?.title).toMatch(
      /approved|completed|headcount/i,
    );
  });
});

describe("position.headcount_requisition.approval dynamic sync/async E2E contract", () => {
  let harness: TestHarness;
  let service: WorkflowService;

  beforeEach(async () => {
    harness = await createHarness();
    service = createWorkflowServiceClient(harness.workflowApi);
  });

  afterEach(async () => {
    await harness.workflowApi.close();
  });

  it("runs the full sequential leadership and async quorum happy path", async () => {
    const workflowInstanceId = await startHeadcountWorkflow(
      service,
      harness,
      "idem_headcount_start_happy",
    );

    const submittedWorkflow = await submitHeadcountWorkflow(
      service,
      harness,
      workflowInstanceId,
      headcountInputFixture(),
      "idem_headcount_submit_happy",
    );
    expect(submittedWorkflow["state"]).toBe("waiting_sync_approval");

    const firstLeadershipTask = pendingTaskForActorAndGate(
      service,
      harness,
      harness.leadershipApprover1Context,
      headcountFixture.leadershipGateId,
    );
    expect(firstLeadershipTask).toMatchObject({
      assigneeActorId: headcountFixture.leadershipApprover1ActorId,
      status: "pending",
    });
    expect(
      pendingTasksForActorAndGate(
        service,
        harness,
        harness.leadershipApprover2Context,
        headcountFixture.leadershipGateId,
      ),
    ).toHaveLength(0);

    await decideApprovalTask({
      service,
      harness,
      context: harness.leadershipApprover1Context,
      workflowInstanceId,
      task: firstLeadershipTask,
      transition: WORKFLOW_TRANSITIONS.APPROVE,
      idempotencyKey: "idem_headcount_leadership_1",
      comment: "Cambridge Nursing staffing need validated.",
      expectedState: "waiting_sync_approval",
    });

    const secondLeadershipTask = pendingTaskForActorAndGate(
      service,
      harness,
      harness.leadershipApprover2Context,
      headcountFixture.leadershipGateId,
    );
    expect(secondLeadershipTask).toMatchObject({
      assigneeActorId: headcountFixture.leadershipApprover2ActorId,
      status: "pending",
    });

    await decideApprovalTask({
      service,
      harness,
      context: harness.leadershipApprover2Context,
      workflowInstanceId,
      task: secondLeadershipTask,
      transition: WORKFLOW_TRANSITIONS.APPROVE,
      idempotencyKey: "idem_headcount_leadership_2",
      comment: "Approved for Cambridge clinic coverage.",
      expectedState: "waiting_async_approval",
    });

    const asyncTasks = tasksForGate(
      harness,
      workflowInstanceId,
      headcountFixture.asyncGateId,
    );
    expect(asyncTasks).toHaveLength(5);
    expect(asyncTasks.every((task) => task.status === "pending")).toBe(true);
    expect(asyncTasks.map((task) => task.assigneeActorId).sort()).toEqual(
      [
        headcountFixture.financeApproverActorId,
        headcountFixture.hrbpApproverActorId,
        headcountFixture.compensationApproverActorId,
        headcountFixture.medicalDirectorApproverActorId,
        headcountFixture.clinicOpsApproverActorId,
      ].sort(),
    );

    await approveAsyncTask(
      service,
      harness,
      harness.financeContext,
      workflowInstanceId,
      "idem_headcount_finance_approve",
    );
    await approveAsyncTask(
      service,
      harness,
      harness.compensationContext,
      workflowInstanceId,
      "idem_headcount_compensation_approve",
    );
    const approvedWorkflow = await approveAsyncTask(
      service,
      harness,
      harness.clinicOpsContext,
      workflowInstanceId,
      "idem_headcount_clinic_ops_approve",
    );

    expect(approvedWorkflow["state"]).toBe("approved");
    expect(approvedWorkflow["currentInteraction"]).toMatchObject(
      expect.objectContaining({
        title: expect.stringMatching(/ready|execute|approved/i),
      }),
    );

    const postQuorumAsyncTasks = tasksForGate(
      harness,
      workflowInstanceId,
      headcountFixture.asyncGateId,
    );
    const canceledAssignees = postQuorumAsyncTasks
      .filter((task) => task.status === "canceled")
      .map((task) => task.assigneeActorId)
      .sort();
    expect(canceledAssignees).toEqual(
      [
        headcountFixture.hrbpApproverActorId,
        headcountFixture.medicalDirectorApproverActorId,
      ].sort(),
    );

    const executeExpectedVersion = workflowVersion(harness, workflowInstanceId);
    const executedWorkflow = await service.transitionWorkflow(
      harness.dependencies,
      harness.systemContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_headcount_execute",
        expectedVersion: executeExpectedVersion,
        input: {},
      },
    );
    expect(executedWorkflow.ok).toBe(true);
    expect(executedWorkflow.ok && executedWorkflow.value["state"]).toBe("executed");

    const eventCountBeforeExecuteReplay = ledgerEventsForWorkflow(
      harness,
      workflowInstanceId,
    ).length;
    const taskCountBeforeExecuteReplay = harness.repositories.store.approvalTasks.size;
    const replayedExecution = await service.transitionWorkflow(
      harness.dependencies,
      harness.systemContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_headcount_execute",
        expectedVersion: executeExpectedVersion,
        input: {},
      },
    );
    expect(replayedExecution.ok).toBe(true);
    expect(replayedExecution.ok && replayedExecution.value["idempotentReplay"]).toBe(
      true,
    );
    expect(ledgerEventsForWorkflow(harness, workflowInstanceId)).toHaveLength(
      eventCountBeforeExecuteReplay,
    );
    expect(harness.repositories.store.approvalTasks.size).toBe(
      taskCountBeforeExecuteReplay,
    );

    const businessTimeline = unwrapResult(
      await service.getTimeline(
        harness.dependencies,
        harness.requesterContext,
        workflowInstanceId,
      ),
    );
    const eventTypes = (
      businessTimeline["events"] as Array<Record<string, unknown>>
    ).map((event) => event["eventType"]);
    expect(eventTypes).toEqual(
      expect.arrayContaining([
        LEDGER_EVENT_TYPES.HEADCOUNT_REQUISITION_SUBMITTED,
        LEDGER_EVENT_TYPES.APPROVAL_GATE_OPENED,
        LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CREATED,
        LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_DECIDED,
        LEDGER_EVENT_TYPES.APPROVAL_GATE_PASSED,
        LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CANCELED,
        LEDGER_EVENT_TYPES.HEADCOUNT_REQUISITION_APPROVED,
        LEDGER_EVENT_TYPES.HEADCOUNT_REQUISITION_EXECUTED,
        LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED,
      ]),
    );
  });

  it("denies wrong actors and prevents the second leadership approver acting early", async () => {
    const workflowInstanceId = await startAndSubmitHeadcountWorkflow(
      service,
      harness,
      "wrong_actor",
    );
    const firstLeadershipTask = pendingTaskForActorAndGate(
      service,
      harness,
      harness.leadershipApprover1Context,
      headcountFixture.leadershipGateId,
    );

    const wrongActorAttempt = await service.transitionWorkflow(
      harness.dependencies,
      harness.financeContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_headcount_wrong_actor",
        expectedVersion: workflowVersion(harness, workflowInstanceId),
        input: {
          approvalTaskId: firstLeadershipTask.approvalTaskId,
          taskVersion: taskVersion(firstLeadershipTask),
          comment: "Finance cannot approve a leadership task.",
        },
      },
    );
    expect(wrongActorAttempt.ok).toBe(false);
    expect(
      !wrongActorAttempt.ok &&
        ["PERMISSION_DENIED", "INVALID_WORKFLOW_TRANSITION"].includes(
          wrongActorAttempt.error.code,
        ),
    ).toBe(true);

    expect(
      pendingTasksForActorAndGate(
        service,
        harness,
        harness.leadershipApprover2Context,
        headcountFixture.leadershipGateId,
      ),
    ).toHaveLength(0);
    const earlySecondApproverAttempt = await service.transitionWorkflow(
      harness.dependencies,
      harness.leadershipApprover2Context,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_headcount_second_leadership_early",
        expectedVersion: workflowVersion(harness, workflowInstanceId),
        input: {
          approvalTaskId: "approval_task_not_open_yet",
          taskVersion: 1,
          comment: "Second leadership approval before task opens.",
        },
      },
    );
    expect(earlySecondApproverAttempt.ok).toBe(false);
  });

  it("fails immediately when a veto holder rejects the async gate", async () => {
    const workflowInstanceId = await openAsyncGate(service, harness, "veto");
    const medicalDirectorTask = pendingTaskForActorAndGate(
      service,
      harness,
      harness.medicalDirectorContext,
      headcountFixture.asyncGateId,
    );
    const rejectedWorkflow = await decideApprovalTask({
      service,
      harness,
      context: harness.medicalDirectorContext,
      workflowInstanceId,
      task: medicalDirectorTask,
      transition: WORKFLOW_TRANSITIONS.REJECT,
      idempotencyKey: "idem_headcount_medical_veto",
      reason: "clinical_coverage_risk",
      comment: "Cannot support safe clinical coverage.",
      expectedState: "approval_gate_failed",
    });

    expect(["approval_gate_failed", "rejected", "failed"]).toContain(
      String(rejectedWorkflow["state"]),
    );
    expect(
      tasksForGate(harness, workflowInstanceId, headcountFixture.asyncGateId).filter(
        (task) => task.status === "pending",
      ).length,
    ).toBe(0);
    expect(ledgerEventTypes(harness, workflowInstanceId)).toContain(
      LEDGER_EVENT_TYPES.APPROVAL_GATE_FAILED,
    );
  });

  it("fails when async quorum becomes impossible without a veto rejection", async () => {
    const workflowInstanceId = await openAsyncGate(service, harness, "impossible");

    await rejectAsyncTask(
      service,
      harness,
      harness.hrbpContext,
      workflowInstanceId,
      "idem_headcount_hrbp_reject",
    );
    await rejectAsyncTask(
      service,
      harness,
      harness.compensationContext,
      workflowInstanceId,
      "idem_headcount_comp_reject",
    );
    const failedWorkflow = await rejectAsyncTask(
      service,
      harness,
      harness.clinicOpsContext,
      workflowInstanceId,
      "idem_headcount_clinic_ops_reject",
    );

    expect(["approval_gate_failed", "rejected", "failed"]).toContain(
      String(failedWorkflow["state"]),
    );
    expect(ledgerEventTypes(harness, workflowInstanceId)).toContain(
      LEDGER_EVENT_TYPES.APPROVAL_GATE_FAILED,
    );
  });

  it("routes request-more-info to approval repair", async () => {
    const workflowInstanceId = await startAndSubmitHeadcountWorkflow(
      service,
      harness,
      "repair",
    );
    const firstLeadershipTask = pendingTaskForActorAndGate(
      service,
      harness,
      harness.leadershipApprover1Context,
      headcountFixture.leadershipGateId,
    );
    const repairWorkflow = await decideApprovalTask({
      service,
      harness,
      context: harness.leadershipApprover1Context,
      workflowInstanceId,
      task: firstLeadershipTask,
      transition: WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO,
      idempotencyKey: "idem_headcount_more_info",
      reason: "needs_staffing_model",
      comment: "Attach the Cambridge evening-volume staffing model.",
      expectedState: "waiting_approval_repair",
    });

    expect(String(repairWorkflow["state"])).toMatch(/repair/);
    expect(repairWorkflow["currentInteraction"]).toEqual(
      expect.objectContaining({
        title: expect.stringMatching(/repair|revise|more information/i),
      }),
    );
  });

  it("rejects duplicate leadership approvers during submit validation", async () => {
    const workflowInstanceId = await startHeadcountWorkflow(
      service,
      harness,
      "idem_headcount_start_duplicate_leadership",
    );
    const submitResult = await service.transitionWorkflow(
      harness.dependencies,
      harness.requesterContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_headcount_duplicate_leadership",
        expectedVersion: 1,
        input: {
          ...headcountInputFixture(),
          selectedLeadershipApprovers: [
            headcountFixture.leadershipApprover1ActorId,
            headcountFixture.leadershipApprover1ActorId,
          ],
        },
      },
    );

    expect(submitResult.ok).toBe(false);
    expect(!submitResult.ok && submitResult.error.code).toBe("VALIDATION_FAILED");
    expect(tasksForWorkflow(harness, workflowInstanceId)).toHaveLength(0);
  });

  it("returns refetch-oriented UX errors for workflow and task conflicts", async () => {
    const workflowInstanceId = await startAndSubmitHeadcountWorkflow(
      service,
      harness,
      "conflict",
    );
    const firstLeadershipTask = pendingTaskForActorAndGate(
      service,
      harness,
      harness.leadershipApprover1Context,
      headcountFixture.leadershipGateId,
    );
    const staleWorkflowAttempt = await service.transitionWorkflow(
      harness.dependencies,
      harness.leadershipApprover1Context,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_headcount_stale_workflow_version",
        expectedVersion: 1,
        input: {
          approvalTaskId: firstLeadershipTask.approvalTaskId,
          taskVersion: taskVersion(firstLeadershipTask),
          comment: "Stale workflow version.",
        },
      },
    );
    expect(staleWorkflowAttempt.ok).toBe(false);
    expect(!staleWorkflowAttempt.ok && staleWorkflowAttempt.error.code).toBe(
      "VERSION_CONFLICT",
    );
    expect(!staleWorkflowAttempt.ok && staleWorkflowAttempt.error.safeMessage).toMatch(
      /changed|refetch/i,
    );

    const staleTaskAttempt = await service.transitionWorkflow(
      harness.dependencies,
      harness.leadershipApprover1Context,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_headcount_stale_task_version",
        expectedVersion: workflowVersion(harness, workflowInstanceId),
        input: {
          approvalTaskId: firstLeadershipTask.approvalTaskId,
          taskVersion: taskVersion(firstLeadershipTask) - 1,
          comment: "Stale task version.",
        },
      },
    );
    expect(staleTaskAttempt.ok).toBe(false);
    expect(
      !staleTaskAttempt.ok &&
        [
          "VERSION_CONFLICT",
          "VALIDATION_FAILED",
          "INVALID_WORKFLOW_TRANSITION",
        ].includes(staleTaskAttempt.error.code),
    ).toBe(true);
    expect(!staleTaskAttempt.ok && staleTaskAttempt.error.safeMessage).toMatch(
      /task|changed|refetch/i,
    );
  });
});

async function createHarness(): Promise<TestHarness> {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const dependencies: AppDependencies = {
    repositories,
    executorClient: createHeadcountExecutorClient(),
  };

  return {
    dependencies,
    repositories,
    workflowApi: await createWorkflowApiClient(dependencies),
    requesterContext: createRequestContext(
      mustActor(repositories, headcountFixture.requesterActorId),
    ),
    leadershipApprover1Context: createRequestContext(
      mustActor(repositories, headcountFixture.leadershipApprover1ActorId),
    ),
    leadershipApprover2Context: createRequestContext(
      mustActor(repositories, headcountFixture.leadershipApprover2ActorId),
    ),
    financeContext: createRequestContext(
      mustActor(repositories, headcountFixture.financeApproverActorId),
    ),
    hrbpContext: createRequestContext(
      mustActor(repositories, headcountFixture.hrbpApproverActorId),
    ),
    compensationContext: createRequestContext(
      mustActor(repositories, headcountFixture.compensationApproverActorId),
    ),
    medicalDirectorContext: createRequestContext(
      mustActor(repositories, headcountFixture.medicalDirectorApproverActorId),
    ),
    clinicOpsContext: createRequestContext(
      mustActor(repositories, headcountFixture.clinicOpsApproverActorId),
    ),
    systemContext: createRequestContext(
      mustActor(repositories, headcountFixture.systemActorId),
    ),
  };
}

function headcountInputFixture(): Record<string, unknown> {
  return {
    ...headcountFixture.request,
    selectedLeadershipApprovers: [
      headcountFixture.leadershipApprover1ActorId,
      headcountFixture.leadershipApprover2ActorId,
    ],
  };
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

function mustActor(repositories: Repositories, actorId: string): ActorRecord {
  const actorResult = repositories.actors.findById(actorId);

  if (!actorResult.ok) {
    throw new Error(`Missing seeded actor ${actorId}`);
  }

  return actorResult.value;
}

function orgUnitIdByKey(repositories: Repositories, unitKey: string): string {
  const result = repositories.organizationUnits.findByKey(DEMO_IDS.tenantId, unitKey);

  if (!result.ok) {
    throw new Error(`Missing org unit ${unitKey}`);
  }

  return result.value.orgUnitId;
}

function createHeadcountExecutorClient(): ExecutorClient {
  return {
    async executeBlock<TOutput>(
      request: ExecutorRequest,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      const output = request.block.name.endsWith(".preflight")
        ? createHeadcountPreflightOutput(request)
        : createHeadcountPlanOutput(request);

      return ok({
        status: "succeeded",
        output: output as TOutput,
        proposedEvents: [],
        externalCallRequests: [],
        logs: [],
        metrics: {
          durationMs: 1,
        },
      });
    },
  };
}

function createHeadcountPreflightOutput(
  request: ExecutorRequest,
): Record<string, unknown> {
  const requestedFte = Number(request.input["requestedFte"]);
  const salaryRangeMin = Number(request.input["salaryRangeMin"]);
  const salaryRangeMax = Number(request.input["salaryRangeMax"]);
  const selectedLeadershipApprovers = Array.isArray(
    request.input["selectedLeadershipApprovers"],
  )
    ? request.input["selectedLeadershipApprovers"]
    : [];
  const hasDuplicateLeadershipApprovers =
    new Set(selectedLeadershipApprovers).size !== selectedLeadershipApprovers.length;
  const errors: Record<string, unknown>[] = [];

  if (!(requestedFte > 0)) {
    errors.push({
      code: "headcount.requested_fte.invalid",
      field: "requestedFte",
    });
  }

  if (salaryRangeMin > salaryRangeMax) {
    errors.push({
      code: "headcount.salary_range.invalid",
      field: "salaryRangeMax",
    });
  }

  if (selectedLeadershipApprovers.length === 0) {
    errors.push({
      code: "headcount.leadership_approvers.required",
      field: "selectedLeadershipApprovers",
    });
  }

  if (hasDuplicateLeadershipApprovers) {
    errors.push({
      code: "headcount.leadership_approvers.duplicate",
      field: "selectedLeadershipApprovers",
    });
  }

  return {
    valid: errors.length === 0,
    riskLevel: "medium",
    requiresEvidence: false,
    requiresApproval: true,
    warnings: [],
    errors,
  };
}

function createHeadcountPlanOutput(request: ExecutorRequest): Record<string, unknown> {
  return {
    internalWrites: [
      {
        eventType: LEDGER_EVENT_TYPES.HEADCOUNT_REQUISITION_EXECUTED,
        subjectType: headcountFixture.subjectType,
        subjectId: headcountFixture.subjectId,
        effectiveAt: String(request.input["targetStartDate"] ?? "2026-07-01"),
        payload: {
          request: headcountInputFixture(),
        },
      },
    ],
    projectionPatches: [],
    externalCallRequests: [
      {
        connectionId: "hris",
        operation: "createPosition",
        idempotencyKey: `headcount:${request.workflowInstanceId}:hris:create_position`,
        payload: {
          request: headcountInputFixture(),
        },
        reconciliation: {},
      },
    ],
  };
}

function readHeadcountWorkflowConfig(): WorkflowConfigContract {
  return JSON.parse(readFileSync(configPath, "utf8")) as WorkflowConfigContract;
}

function createWorkflowServiceClient(workflowApi: WorkflowApiClient): WorkflowService {
  return {
    startWorkflowIntent(_dependencies, requestContext, body) {
      return workflowApi.startWorkflowIntent(requestContext, body);
    },
    transitionWorkflow(_dependencies, requestContext, workflowInstanceId, body) {
      return workflowApi.transitionWorkflow(requestContext, workflowInstanceId, body);
    },
    getTasks(dependencies, requestContext) {
      const tasksResult = dependencies.repositories.approvals.findPendingForActor(
        requestContext.actor,
      );
      if (!tasksResult.ok) {
        return tasksResult;
      }

      return ok({ tasks: tasksResult.value });
    },
    getTimeline(_dependencies, requestContext, workflowInstanceId, view) {
      return workflowApi.getTimeline(requestContext, workflowInstanceId, view);
    },
    getAvailableActions(_dependencies, requestContext, workflowInstanceId) {
      return workflowApi.getAvailableActions(requestContext, workflowInstanceId);
    },
  };
}

async function startHeadcountWorkflow(
  service: WorkflowService,
  harness: TestHarness,
  idempotencyKey: string,
): Promise<string> {
  const startedWorkflow = await service.startWorkflowIntent(
    harness.dependencies,
    harness.requesterContext,
    {
      intent: headcountFixture.intent,
      idempotencyKey,
      subjectType: headcountFixture.subjectType,
      subjectId: headcountFixture.subjectId,
    },
  );

  expect(startedWorkflow.ok).toBe(true);
  return String(unwrapResult(startedWorkflow)["workflowInstanceId"]);
}

async function submitHeadcountWorkflow(
  service: WorkflowService,
  harness: TestHarness,
  workflowInstanceId: string,
  input: Record<string, unknown>,
  idempotencyKey: string,
): Promise<Record<string, unknown>> {
  const submittedWorkflow = await service.transitionWorkflow(
    harness.dependencies,
    harness.requesterContext,
    workflowInstanceId,
    {
      transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
      idempotencyKey,
      expectedVersion: 1,
      input,
    },
  );

  if (!submittedWorkflow.ok) {
    throw new Error(JSON.stringify(submittedWorkflow.error));
  }

  return unwrapResult(submittedWorkflow);
}

async function startAndSubmitHeadcountWorkflow(
  service: WorkflowService,
  harness: TestHarness,
  keySuffix: string,
): Promise<string> {
  const workflowInstanceId = await startHeadcountWorkflow(
    service,
    harness,
    `idem_headcount_start_${keySuffix}`,
  );

  await submitHeadcountWorkflow(
    service,
    harness,
    workflowInstanceId,
    headcountInputFixture(),
    `idem_headcount_submit_${keySuffix}`,
  );

  return workflowInstanceId;
}

async function openAsyncGate(
  service: WorkflowService,
  harness: TestHarness,
  keySuffix: string,
): Promise<string> {
  const workflowInstanceId = await startAndSubmitHeadcountWorkflow(
    service,
    harness,
    keySuffix,
  );
  const firstTask = pendingTaskForActorAndGate(
    service,
    harness,
    harness.leadershipApprover1Context,
    headcountFixture.leadershipGateId,
  );
  await decideApprovalTask({
    service,
    harness,
    context: harness.leadershipApprover1Context,
    workflowInstanceId,
    task: firstTask,
    transition: WORKFLOW_TRANSITIONS.APPROVE,
    idempotencyKey: `idem_headcount_leadership_1_${keySuffix}`,
    comment: "Leadership stage one approved.",
    expectedState: "waiting_sync_approval",
  });

  const secondTask = pendingTaskForActorAndGate(
    service,
    harness,
    harness.leadershipApprover2Context,
    headcountFixture.leadershipGateId,
  );
  await decideApprovalTask({
    service,
    harness,
    context: harness.leadershipApprover2Context,
    workflowInstanceId,
    task: secondTask,
    transition: WORKFLOW_TRANSITIONS.APPROVE,
    idempotencyKey: `idem_headcount_leadership_2_${keySuffix}`,
    comment: "Leadership stage two approved.",
    expectedState: "waiting_async_approval",
  });

  return workflowInstanceId;
}

async function approveAsyncTask(
  service: WorkflowService,
  harness: TestHarness,
  context: ApiRequestContext,
  workflowInstanceId: string,
  idempotencyKey: string,
): Promise<Record<string, unknown>> {
  const task = pendingTaskForActorAndGate(
    service,
    harness,
    context,
    headcountFixture.asyncGateId,
  );

  return decideApprovalTask({
    service,
    harness,
    context,
    workflowInstanceId,
    task,
    transition: WORKFLOW_TRANSITIONS.APPROVE,
    idempotencyKey,
    comment: "Async gate approved.",
  });
}

async function rejectAsyncTask(
  service: WorkflowService,
  harness: TestHarness,
  context: ApiRequestContext,
  workflowInstanceId: string,
  idempotencyKey: string,
): Promise<Record<string, unknown>> {
  const task = pendingTaskForActorAndGate(
    service,
    harness,
    context,
    headcountFixture.asyncGateId,
  );

  return decideApprovalTask({
    service,
    harness,
    context,
    workflowInstanceId,
    task,
    transition: WORKFLOW_TRANSITIONS.REJECT,
    idempotencyKey,
    reason: "quorum_not_supported",
    comment: "Async gate rejected.",
  });
}

async function decideApprovalTask(input: {
  service: WorkflowService;
  harness: TestHarness;
  context: ApiRequestContext;
  workflowInstanceId: string;
  task: ApprovalTaskRecord;
  transition: string;
  idempotencyKey: string;
  comment?: string;
  reason?: string;
  expectedState?: string;
}): Promise<Record<string, unknown>> {
  const result = await transitionWithCurrentVersion(
    input.service,
    input.harness,
    input.context,
    input.workflowInstanceId,
    {
      transition: input.transition,
      idempotencyKey: input.idempotencyKey,
      input: {
        approvalTaskId: input.task.approvalTaskId,
        taskVersion: taskVersion(input.task),
        ...(input.comment !== undefined ? { comment: input.comment } : {}),
        ...(input.reason !== undefined ? { reason: input.reason } : {}),
      },
    },
  );

  expect(result.ok).toBe(true);
  const value = unwrapResult(result);
  if (input.expectedState !== undefined) {
    expect(value["state"]).toBe(input.expectedState);
  }

  return value;
}

async function transitionWithCurrentVersion(
  service: WorkflowService,
  harness: TestHarness,
  context: ApiRequestContext,
  workflowInstanceId: string,
  input: {
    transition: string;
    idempotencyKey: string;
    input: Record<string, unknown>;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  return service.transitionWorkflow(harness.dependencies, context, workflowInstanceId, {
    transition: input.transition,
    idempotencyKey: input.idempotencyKey,
    expectedVersion: workflowVersion(harness, workflowInstanceId),
    input: input.input,
  });
}

function pendingTaskForActorAndGate(
  service: WorkflowService,
  harness: TestHarness,
  requestContext: ApiRequestContext,
  gateId: string,
): ApprovalTaskRecord {
  const task = pendingTasksForActorAndGate(service, harness, requestContext, gateId)[0];

  if (task === undefined) {
    throw new Error(
      `Missing pending ${gateId} task for ${requestContext.actor.actorId}`,
    );
  }

  return task;
}

function pendingTasksForActorAndGate(
  service: WorkflowService,
  harness: TestHarness,
  requestContext: ApiRequestContext,
  gateId: string,
): ApprovalTaskRecord[] {
  const tasksResult = service.getTasks(harness.dependencies, requestContext);
  const taskIds = unwrapResult(tasksResult)["tasks"] as Array<Record<string, unknown>>;

  return taskIds
    .map((task) => {
      return harness.repositories.store.approvalTasks.get(
        String(task["approvalTaskId"]),
      );
    })
    .filter((task): task is ApprovalTaskRecord => {
      return task !== undefined && isTaskForGate(task, gateId);
    });
}

function tasksForWorkflow(
  harness: TestHarness,
  workflowInstanceId: string,
): ApprovalTaskRecord[] {
  return [...harness.repositories.store.approvalTasks.values()].filter((task) => {
    return task.workflowInstanceId === workflowInstanceId;
  });
}

function tasksForGate(
  harness: TestHarness,
  workflowInstanceId: string,
  gateId: string,
): ApprovalTaskRecord[] {
  return tasksForWorkflow(harness, workflowInstanceId).filter((task) => {
    return isTaskForGate(task, gateId);
  });
}

function isTaskForGate(task: ApprovalTaskRecord, gateId: string): boolean {
  const metadata = task.metadata;

  return (
    metadata["approvalGateId"] === gateId ||
    metadata["approvalGroupId"] === gateId ||
    metadata["gateId"] === gateId ||
    metadata["gateNodeId"] === gateId ||
    task.approvalType === gateId
  );
}

function taskVersion(task: ApprovalTaskRecord): number {
  const version = task.metadata["taskVersion"];

  return typeof version === "number" ? version : 1;
}

function workflowVersion(harness: TestHarness, workflowInstanceId: string): number {
  const workflowInstance =
    harness.repositories.store.workflowInstances.get(workflowInstanceId);

  if (workflowInstance === undefined) {
    throw new Error(`Missing workflow instance ${workflowInstanceId}`);
  }

  return workflowInstance.version;
}

function ledgerEventsForWorkflow(
  harness: TestHarness,
  workflowInstanceId: string,
): Array<Record<string, unknown>> {
  return harness.repositories.store.ledgerEvents.filter((event) => {
    return event.workflowInstanceId === workflowInstanceId;
  });
}

function ledgerEventTypes(harness: TestHarness, workflowInstanceId: string): string[] {
  return ledgerEventsForWorkflow(harness, workflowInstanceId).map((event) => {
    return String(event["eventType"]);
  });
}

function unwrapResult<TValue>(result: Result<TValue, AppError>): TValue {
  if (!result.ok) {
    throw new Error(result.error.message);
  }

  return result.value;
}

function isHumanLabel(label: string): boolean {
  return !label.includes("_") && label.trim().length > 2;
}

function stringRecord(value: unknown): Record<string, string> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return {};
  }

  return Object.fromEntries(
    Object.entries(value).filter((entry): entry is [string, string] => {
      return typeof entry[1] === "string";
    }),
  );
}
