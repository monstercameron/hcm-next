import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  ACTOR_TYPES,
  INTEGRATION_OUTBOX_STATUSES,
  LEDGER_EVENT_TYPES,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  type AppError,
  type Result,
  ok,
} from "@hcm-next/foundation";
import {
  createInitialWorkflowInstance,
  createRepositories,
  createSeededDemoStore,
  DEMO_IDS,
  nowIso,
  type ApprovalTaskRecord,
  type IntegrationOutboxRecord,
  type WorkflowInstanceRecord,
  type Repositories,
} from "@hcm-next/data-store";
import { createApiServer } from "../../api/server.js";
import type { AppDependencies } from "../../api/dependencies.js";
import type {
  ExecutorClient,
  ExecutorRequest,
  ExecutorResponse,
} from "../../api/executor-client.js";
import { findFilesystemWorkflowConfigByIntent } from "../../workflows/shared/workflow-config-registry.js";
import type {
  WorkflowConfig,
  WorkflowGraphNodeConfig,
} from "../../workflows/shared/workflow-config.js";

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  apiServer: Server;
  apiOrigin: string;
};

type JsonResponse = {
  status: number;
  body: Record<string, unknown>;
};

describe("workflow admin JSON routes", () => {
  let harness: TestHarness | undefined;

  beforeEach(async () => {
    harness = await createHarness();
  });

  afterEach(async () => {
    if (harness !== undefined) {
      await closeServer(harness.apiServer);
      harness = undefined;
    }
  });

  it("exposes validation, Mermaid, block catalog, preview, permission, simulation, diff, and guardrail endpoints", async () => {
    const activeHarness = requireHarness(harness);
    const workflowConfig = legalNameWorkflowConfig();
    const preflightNode = requireNode(workflowConfig, "legal_name_preflight");

    const validationResponse = await adminPost(
      activeHarness,
      "/admin/workflows/validate-json",
      { workflowConfig },
    );
    expect(validationResponse.status).toBe(200);
    expect(recordField(validationResponse.body, "validation")["valid"]).toBe(true);
    expect(validationResponse.body["correlationId"]).toBe("corr_admin");

    const mermaidResponse = await adminPost(
      activeHarness,
      "/admin/workflows/mermaid-preview",
      { workflowConfig },
    );
    expect(mermaidResponse.status).toBe(200);
    expect(String(mermaidResponse.body["mermaid"])).toContain("flowchart LR");

    const blockCatalogResponse = await adminGet(
      activeHarness,
      "/admin/workflow-blocks",
    );
    expect(blockCatalogResponse.status).toBe(200);
    expect(blockCatalogResponse.body["blocks"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          name: "system.employee_data.legal_name.preflight",
        }),
      ]),
    );

    const inputMappingResponse = await adminPost(
      activeHarness,
      "/admin/workflows/input-mapping-preview",
      {
        workflowConfig,
        nodeId: preflightNode.nodeId,
        sources: {
          workflowInput: legalNameInput(),
          employeeProjection: employeeDocument(activeHarness),
        },
      },
    );
    expect(inputMappingResponse.status).toBe(200);
    expect(inputMappingResponse.body["nodeId"]).toBe(preflightNode.nodeId);

    const interactionResponse = await adminPost(
      activeHarness,
      "/admin/workflows/interaction-preview",
      {
        workflowConfig,
        state: "collecting_input",
        employeeFixture: employeeDocument(activeHarness),
      },
    );
    expect(interactionResponse.status).toBe(200);
    expect(interactionResponse.body["interactionKey"]).toBe("input");
    expect(interactionResponse.body["fields"]).toEqual(
      expect.arrayContaining([expect.objectContaining({ path: "newLegalName.first" })]),
    );

    const permissionResponse = await adminPost(
      activeHarness,
      "/admin/workflows/permission-preview",
      {
        workflowConfig,
        employeeId: DEMO_IDS.employeeId,
        workflowState: "collecting_input",
      },
    );
    expect(permissionResponse.status).toBe(200);
    expect(permissionResponse.body["workflowIntent"]).toBe(workflowConfig.intent);

    const simulationResponse = await adminPost(
      activeHarness,
      "/admin/workflows/simulate",
      {
        workflowConfig,
        employeeId: DEMO_IDS.employeeId,
        workflowInput: legalNameInput(),
      },
    );
    expect(simulationResponse.status).toBe(200);
    expect(simulationResponse.body["durableWritesCreated"]).toBe(false);
    expect(simulationResponse.body["traces"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ nodeId: "collect_legal_name_input" }),
      ]),
    );

    const diffResponse = await adminPost(activeHarness, "/admin/workflows/diff", {
      draftConfig: workflowConfig,
      publishedConfig: workflowConfig,
    });
    expect(diffResponse.status).toBe(200);
    expect(diffResponse.body["hasChanges"]).toBe(false);
    expect(diffResponse.body["items"]).toEqual([]);

    const guardrailResponse = await adminPost(
      activeHarness,
      "/admin/workflows/publish-guardrails",
      { workflowConfig },
    );
    expect(guardrailResponse.status).toBe(200);
    expect(guardrailResponse.body["canPublish"]).toBe(false);
    expect(guardrailResponse.body["errors"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          code: "publish_guardrail.integration_bindings_missing",
        }),
      ]),
    );

    const bindingCreateResponse = await adminPost(
      activeHarness,
      "/admin/workflow-integration-bindings",
      hrisIntegrationBindingPayload({ timeoutMs: 7000 }),
    );
    expect(bindingCreateResponse.status).toBe(200);
    expect(recordField(bindingCreateResponse.body, "binding")["timeoutMs"]).toBe(7000);

    const bindingUpdateResponse = await adminPost(
      activeHarness,
      "/admin/workflow-integration-bindings",
      hrisIntegrationBindingPayload({ timeoutMs: 8000 }),
    );
    expect(bindingUpdateResponse.status).toBe(200);
    expect(recordField(bindingUpdateResponse.body, "binding")["timeoutMs"]).toBe(8000);

    const bindingListResponse = await adminGet(
      activeHarness,
      "/admin/workflow-integration-bindings",
    );
    expect(bindingListResponse.status).toBe(200);
    expect(bindingListResponse.body["bindings"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          abstractConnectionId: "hris",
          timeoutMs: 8000,
        }),
      ]),
    );
  });

  it("creates, publishes, starts from DB config, debugs, and repairs a workflow instance", async () => {
    const activeHarness = requireHarness(harness);
    const workflowConfig = legalNameWorkflowConfig();

    const createDraftResponse = await adminPost(
      activeHarness,
      "/admin/workflow-drafts",
      { workflowConfig },
    );
    expect(createDraftResponse.status).toBe(200);

    const draft = recordField(createDraftResponse.body, "draft");
    const family = recordField(createDraftResponse.body, "family");
    const workflowDraftId = stringField(draft, "workflowDraftId");
    const workflowFamilyId = stringField(family, "workflowFamilyId");
    expect(workflowDraftId).toBeTruthy();
    expect(workflowFamilyId).toBeTruthy();

    const publishResponse = await adminPost(
      activeHarness,
      `/admin/workflow-drafts/${workflowDraftId}/publish`,
      { expectedVersion: 1 },
    );
    expect(publishResponse.status).toBe(200);

    const publish = recordField(publishResponse.body, "publish");
    const publishedVersion = recordField(publish, "publishedVersion");
    const publishedVersionId = stringField(publishedVersion, "workflowVersionRecordId");
    expect(publishedVersionId).toBeTruthy();

    const familyListResponse = await adminGet(
      activeHarness,
      "/admin/workflow-families",
    );
    expect(familyListResponse.status).toBe(200);
    expect(familyListResponse.body["workflows"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          workflowFamilyId,
          activeWorkflowVersionRecordId: publishedVersionId,
        }),
      ]),
    );

    const startResponse = await employeePost(activeHarness, "/workflow-intents", {
      intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      subjectType: "worker",
      subjectId: DEMO_IDS.employeeId,
    });
    expect(startResponse.status).toBe(200);
    expect(startResponse.body["workflowDefinitionId"]).toBe(workflowFamilyId);
    expect(startResponse.body["workflowVersionId"]).toBe(publishedVersionId);

    const workflowInstanceId = String(startResponse.body["workflowInstanceId"]);
    const debuggerResponse = await adminGet(
      activeHarness,
      `/admin/workflow-instances/${workflowInstanceId}/debug`,
    );
    expect(debuggerResponse.status).toBe(200);
    expect(debuggerResponse.body["workflowInstanceId"]).toBe(workflowInstanceId);
    expect(debuggerResponse.body["repairOptions"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ action: "cancel_workflow", enabled: true }),
      ]),
    );

    const repairResponse = await adminPost(
      activeHarness,
      `/admin/workflow-instances/${workflowInstanceId}/repair-actions`,
      {
        action: "cancel_workflow",
        idempotencyKey: "admin_route_cancel_legal_name",
        expectedVersion: 1,
      },
    );
    expect(repairResponse.status).toBe(200);
    expect(repairResponse.body["accepted"]).toBe(true);

    const workflowInstance =
      activeHarness.repositories.store.workflowInstances.get(workflowInstanceId);
    expect(workflowInstance?.status).toBe("canceled");
  });

  it("covers template cloning, draft simulation, version pinning, debugger, repair, rollback, archive, isolation, and publish blockers", async () => {
    const activeHarness = requireHarness(harness);
    const workflowConfig = legalNameWorkflowConfig();
    const compensationWorkflowConfig = compensationWorkflowConfigFixture();

    const seedResponse = await adminPost(
      activeHarness,
      "/admin/workflow-templates/seed",
      {},
    );
    expect(seedResponse.status).toBe(200);
    expect(Number(seedResponse.body["seeded"])).toBeGreaterThan(0);
    expectAdminContract(seedResponse.body);

    const templateListResponse = await adminGet(
      activeHarness,
      "/admin/workflow-templates",
    );
    expect(templateListResponse.status).toBe(200);
    expectAdminContract(templateListResponse.body);

    const legalNameTemplate = arrayField(templateListResponse.body, "templates").find(
      (template) => {
        return template["intent"] === WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE;
      },
    );
    expect(legalNameTemplate).toBeDefined();

    const cloneTemplateResponse = await adminPost(
      activeHarness,
      `/admin/workflow-templates/${String(legalNameTemplate?.["workflowTemplateId"])}/clone`,
      {},
    );
    expect(cloneTemplateResponse.status).toBe(200);

    const clonedDraft = recordField(cloneTemplateResponse.body, "draft");
    const clonedFamily = recordField(cloneTemplateResponse.body, "family");
    const workflowDraftId = stringField(clonedDraft, "workflowDraftId");
    const workflowFamilyId = stringField(clonedFamily, "workflowFamilyId");
    const editedWorkflowConfig = workflowConfigWithTitle(
      workflowConfig,
      "Employee Legal Name Change Admin v1",
    );

    const saveResponse = await adminPost(
      activeHarness,
      `/admin/workflow-drafts/${workflowDraftId}/save`,
      {
        expectedVersion: numberField(clonedDraft, "version"),
        workflowConfig: editedWorkflowConfig,
      },
    );
    expect(saveResponse.status).toBe(200);

    const draftSimulationResponse = await adminPost(
      activeHarness,
      "/admin/workflows/simulate",
      {
        workflowDraftId,
        employeeId: DEMO_IDS.employeeId,
        workflowInput: legalNameInput(),
      },
    );
    expect(draftSimulationResponse.status).toBe(200);
    expect(draftSimulationResponse.body["workflowIntent"]).toBe(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );

    const publishV1Response = await adminPost(
      activeHarness,
      `/admin/workflow-drafts/${workflowDraftId}/publish`,
      {
        expectedVersion: numberField(
          recordField(saveResponse.body, "draft"),
          "version",
        ),
      },
    );
    expect(publishV1Response.status).toBe(200);

    const publishV1 = recordField(publishV1Response.body, "publish");
    const publishedV1 = recordField(publishV1, "publishedVersion");
    const publishedV1Id = stringField(publishedV1, "workflowVersionRecordId");

    const startedV1Response = await employeePost(activeHarness, "/workflow-intents", {
      intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      subjectType: "worker",
      subjectId: DEMO_IDS.employeeId,
    });
    expect(startedV1Response.status).toBe(200);
    expect(startedV1Response.body["workflowVersionId"]).toBe(publishedV1Id);

    const workflowInstanceId = String(startedV1Response.body["workflowInstanceId"]);
    moveWorkflowToWaitingApproval(activeHarness, workflowInstanceId);

    const waitingDebuggerResponse = await adminGet(
      activeHarness,
      `/admin/workflow-instances/${workflowInstanceId}/debug`,
    );
    expect(waitingDebuggerResponse.status).toBe(200);
    expect(waitingDebuggerResponse.body["pendingApprovalTasks"]).toEqual(
      expect.arrayContaining([expect.objectContaining({ status: "pending" })]),
    );
    expect(waitingDebuggerResponse.body["stuckDetections"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ code: "waiting_approval_beyond_threshold" }),
      ]),
    );

    const clonePublishedResponse = await adminPost(
      activeHarness,
      `/admin/published-workflow-versions/${publishedV1Id}/clone`,
      {},
    );
    expect(clonePublishedResponse.status).toBe(200);

    const versionDraft = recordField(clonePublishedResponse.body, "draft");
    const saveV2Response = await adminPost(
      activeHarness,
      `/admin/workflow-drafts/${stringField(versionDraft, "workflowDraftId")}/save`,
      {
        expectedVersion: numberField(versionDraft, "version"),
        workflowConfig: workflowConfigWithTitle(
          workflowConfig,
          "Employee Legal Name Change Admin v2",
        ),
      },
    );
    expect(saveV2Response.status).toBe(200);

    const publishV2Response = await adminPost(
      activeHarness,
      `/admin/workflow-drafts/${stringField(versionDraft, "workflowDraftId")}/publish`,
      {
        expectedVersion: numberField(
          recordField(saveV2Response.body, "draft"),
          "version",
        ),
      },
    );
    expect(publishV2Response.status).toBe(200);

    const publishedV2 = recordField(
      recordField(publishV2Response.body, "publish"),
      "publishedVersion",
    );
    const publishedV2Id = stringField(publishedV2, "workflowVersionRecordId");
    const pinnedWorkflow =
      activeHarness.repositories.store.workflowInstances.get(workflowInstanceId);
    expect(pinnedWorkflow?.workflowVersionId).toBe(publishedV1Id);

    const startedV2Response = await employeePost(activeHarness, "/workflow-intents", {
      intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      subjectType: "worker",
      subjectId: DEMO_IDS.employeeId,
    });
    expect(startedV2Response.status).toBe(200);
    expect(startedV2Response.body["workflowVersionId"]).toBe(publishedV2Id);

    createFailedIntegrationDebugFixture(activeHarness, compensationWorkflowConfig);
    const failedDebuggerResponse = await adminGet(
      activeHarness,
      "/admin/workflow-instances/workflow_failed_debug/debug",
    );
    expect(failedDebuggerResponse.status).toBe(200);
    expect(failedDebuggerResponse.body["failure"]).toMatchObject({
      failedNodeId: "vendor_compensation_decision",
      errorCode: "COMP_VENDOR_REJECTED",
      safeMessage: "The compensation vendor rejected the payload.",
    });
    expect(failedDebuggerResponse.body["compensationEligibility"]).toMatchObject({
      eligible: true,
    });
    expect(JSON.stringify(failedDebuggerResponse.body)).toContain("[redacted]");
    expect(failedDebuggerResponse.body["repairOptions"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ action: "retry_integration", enabled: true }),
        expect.objectContaining({ action: "reopen_repair", enabled: true }),
      ]),
    );
    expect(failedDebuggerResponse.body["nodeExecutionHistory"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          nodeId: "vendor_compensation_decision",
          routeDecision: expect.objectContaining({
            routeKeys: expect.arrayContaining(["rejected"]),
          }),
        }),
      ]),
    );

    const staleRepairResponse = await adminPost(
      activeHarness,
      "/admin/workflow-instances/workflow_failed_debug/repair-actions",
      {
        action: "reopen_repair",
        idempotencyKey: "idem_failed_debug_reopen_stale",
        expectedVersion: 2,
      },
    );
    expect(staleRepairResponse.status).toBe(409);

    const retryRepairResponse = await adminPost(
      activeHarness,
      "/admin/workflow-instances/workflow_failed_debug/repair-actions",
      {
        action: "retry_integration",
        idempotencyKey: "idem_failed_debug_retry",
        expectedVersion: 3,
      },
    );
    expect(retryRepairResponse.status).toBe(200);
    expect(retryRepairResponse.body["accepted"]).toBe(true);
    expect(
      activeHarness.repositories.store.integrationOutbox.get("outbox_failed_debug")
        ?.status,
    ).toBe(INTEGRATION_OUTBOX_STATUSES.PENDING);

    const reopenRepairResponse = await adminPost(
      activeHarness,
      "/admin/workflow-instances/workflow_failed_debug/repair-actions",
      {
        action: "reopen_repair",
        idempotencyKey: "idem_failed_debug_reopen",
        expectedVersion: 3,
      },
    );
    expect(reopenRepairResponse.status).toBe(200);
    expect(
      activeHarness.repositories.store.workflowInstances.get("workflow_failed_debug")
        ?.version,
    ).toBe(4);

    const rollbackResponse = await adminPost(
      activeHarness,
      `/admin/workflow-families/${workflowFamilyId}/rollback`,
      {
        targetWorkflowVersionRecordId: publishedV1Id,
        expectedFamilyVersion: numberField(
          recordField(recordField(publishV2Response.body, "publish"), "family"),
          "version",
        ),
      },
    );
    expect(rollbackResponse.status).toBe(200);
    expect(
      recordField(rollbackResponse.body, "activeVersion")["workflowVersionRecordId"],
    ).toBe(publishedV1Id);

    createOtherTenantFamily(activeHarness);
    const listAfterOtherTenantResponse = await adminGet(
      activeHarness,
      "/admin/workflow-families",
    );
    expect(listAfterOtherTenantResponse.status).toBe(200);
    expect(JSON.stringify(listAfterOtherTenantResponse.body)).not.toContain(
      "workflow_family_other_tenant",
    );

    const otherTenantDetailResponse = await adminGet(
      activeHarness,
      "/admin/workflow-families/workflow_family_other_tenant",
    );
    expect(otherTenantDetailResponse.status).toBe(404);

    const missingBlockResponse = await adminPost(
      activeHarness,
      "/admin/workflows/publish-guardrails",
      {
        workflowConfig: workflowConfigWithMissingBlock(workflowConfig),
      },
    );
    expect(missingBlockResponse.status).toBe(200);
    expect(missingBlockResponse.body["errors"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ code: "publish_guardrail.block_missing" }),
      ]),
    );

    const unreachableNodeResponse = await adminPost(
      activeHarness,
      "/admin/workflows/publish-guardrails",
      {
        workflowConfig: workflowConfigWithUnreachableNode(workflowConfig),
      },
    );
    expect(unreachableNodeResponse.status).toBe(200);
    expect(unreachableNodeResponse.body["errors"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          code: "publish_guardrail.required_node_unreachable",
        }),
      ]),
    );

    const archiveResponse = await adminPost(
      activeHarness,
      `/admin/workflow-families/${workflowFamilyId}/archive`,
      {
        expectedVersion: numberField(
          recordField(rollbackResponse.body, "family"),
          "version",
        ),
      },
    );
    expect(archiveResponse.status).toBe(200);
    expect(recordField(archiveResponse.body, "family")["status"]).toBe("archived");
  });
});

async function createHarness(): Promise<TestHarness> {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const dependencies: AppDependencies = {
    repositories,
    executorClient: createFakeExecutorClient(),
  };
  const apiServer = createApiServer(dependencies);
  const apiOrigin = await startTestServer(apiServer);

  return {
    dependencies,
    repositories,
    apiServer,
    apiOrigin,
  };
}

function createFakeExecutorClient(): ExecutorClient {
  return {
    async executeBlock<TOutput>(
      request: ExecutorRequest,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      return ok({
        status: "succeeded",
        output: {
          valid: true,
          routeKey: "ok",
          warnings: [],
          errors: [],
          request,
        } as TOutput,
        proposedEvents: [],
        externalCallRequests: [],
        logs: [],
        metrics: { durationMs: 1 },
      });
    },
  };
}

async function adminGet(harness: TestHarness, path: string): Promise<JsonResponse> {
  return httpJson(harness, "GET", path, DEMO_IDS.hrActorId);
}

async function adminPost(
  harness: TestHarness,
  path: string,
  body: Record<string, unknown>,
): Promise<JsonResponse> {
  return httpJson(harness, "POST", path, DEMO_IDS.hrActorId, body);
}

async function employeePost(
  harness: TestHarness,
  path: string,
  body: Record<string, unknown>,
): Promise<JsonResponse> {
  return httpJson(harness, "POST", path, DEMO_IDS.employeeActorId, body);
}

async function httpJson(
  harness: TestHarness,
  method: "GET" | "POST",
  path: string,
  actorId: string,
  body?: Record<string, unknown>,
): Promise<JsonResponse> {
  const response = await fetch(`${harness.apiOrigin}${path}`, {
    method,
    headers: {
      "content-type": "application/json",
      "x-demo-actor-id": actorId,
      "x-request-id": `req_${actorId}`,
      "x-correlation-id": "corr_admin",
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const responseBody = (await response.json()) as Record<string, unknown>;

  return {
    status: response.status,
    body: responseBody,
  };
}

function legalNameWorkflowConfig(): WorkflowConfig {
  const workflowConfigResult = findFilesystemWorkflowConfigByIntent(
    WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
  );

  if (!workflowConfigResult.ok) {
    throw new Error(workflowConfigResult.error.safeMessage);
  }

  return workflowConfigResult.value;
}

function compensationWorkflowConfigFixture(): WorkflowConfig {
  const workflowConfigResult = findFilesystemWorkflowConfigByIntent(
    WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
  );

  if (!workflowConfigResult.ok) {
    throw new Error(workflowConfigResult.error.safeMessage);
  }

  return workflowConfigResult.value;
}

function workflowConfigWithTitle(
  workflowConfig: WorkflowConfig,
  title: string,
): WorkflowConfig {
  const clonedWorkflowConfig = cloneWorkflowConfig(workflowConfig);

  return {
    ...clonedWorkflowConfig,
    metadata: {
      ...clonedWorkflowConfig.metadata,
      title,
    },
  };
}

function workflowConfigWithMissingBlock(
  workflowConfig: WorkflowConfig,
): WorkflowConfig {
  const clonedWorkflowConfig = cloneWorkflowConfig(workflowConfig);

  clonedWorkflowConfig.submit = {
    ...clonedWorkflowConfig.submit,
    preflightBlock: {
      name: "missing.block",
      version: "0.0.1",
      runtime: "go",
    },
  };

  return clonedWorkflowConfig;
}

function workflowConfigWithUnreachableNode(
  workflowConfig: WorkflowConfig,
): WorkflowConfig {
  const clonedWorkflowConfig = cloneWorkflowConfig(workflowConfig);
  const graph = clonedWorkflowConfig.graph;

  if (graph === undefined) {
    throw new Error("Expected workflow graph.");
  }

  graph.nodes.push({
    nodeId: "unreachable_required_review",
    type: "block",
    title: "Unreachable required review",
    block: {
      name: "system.employee_data.legal_name.preflight",
      version: "1.0.0",
      runtime: "go",
    },
    outcomes: [
      {
        outcome: "valid",
        routeKey: "valid",
        nextNodeId: "completed",
      },
    ],
  });

  return clonedWorkflowConfig;
}

function cloneWorkflowConfig(workflowConfig: WorkflowConfig): WorkflowConfig {
  return JSON.parse(JSON.stringify(workflowConfig)) as WorkflowConfig;
}

function hrisIntegrationBindingPayload(input: {
  timeoutMs: number;
}): Record<string, unknown> {
  return {
    abstractConnectionId: "hris",
    connectorId: "connector_hris",
    connectorEnvironment: "production",
    secretRef: "secret_hris",
    enabled: true,
    allowedOperations: ["updateLegalName", "createPosition"],
    timeoutMs: input.timeoutMs,
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
}

function moveWorkflowToWaitingApproval(
  harness: TestHarness,
  workflowInstanceId: string,
): void {
  const workflowInstance = requireWorkflowInstance(harness, workflowInstanceId);
  const waitingApprovalWorkflow: WorkflowInstanceRecord = {
    ...workflowInstance,
    state: WORKFLOW_STATES.WAITING_APPROVAL,
    status: WORKFLOW_STATUSES.WAITING,
    currentInteraction: {
      type: "waiting",
      title: "Waiting for approval",
    },
    changeRequestId: "change_request_waiting_debug",
    version: 2,
    updatedAt: nowIso(),
  };
  const approvalTask: ApprovalTaskRecord = {
    approvalTaskId: "approval_task_waiting_debug",
    tenantId: DEMO_IDS.tenantId,
    changeRequestId: "change_request_waiting_debug",
    workflowInstanceId,
    assigneeActorId: DEMO_IDS.hrActorId,
    assigneeRole: "hr_admin",
    approvalType: "legal_name_review",
    status: "pending",
    createdAt: "2026-05-01T00:00:00.000Z",
    metadata: {},
  };

  harness.repositories.store.workflowInstances.set(
    workflowInstanceId,
    waitingApprovalWorkflow,
  );
  harness.repositories.store.approvalTasks.set(
    approvalTask.approvalTaskId,
    approvalTask,
  );
}

function createFailedIntegrationDebugFixture(
  harness: TestHarness,
  workflowConfig: WorkflowConfig,
): void {
  const workflowInstance = createInitialWorkflowInstance({
    tenantId: DEMO_IDS.tenantId,
    environmentId: DEMO_IDS.environmentId,
    workflowDefinitionId: DEMO_IDS.compensationWorkflowDefinitionId,
    workflowVersionId: DEMO_IDS.compensationWorkflowVersionId,
    intent: workflowConfig.intent,
    subjectType: "worker",
    subjectId: DEMO_IDS.employeeId,
    requesterActorId: DEMO_IDS.hrActorId,
    currentInteraction: {
      type: "repair",
      title: "Vendor decision needs repair",
      jsonSchema: {
        type: "object",
        required: ["repairAction"],
      },
    },
    context: {
      activeNodeId: "vendor_compensation_decision",
    },
    correlationId: "corr_failed_debug",
    metadata: {},
  });
  const waitingRepairWorkflow: WorkflowInstanceRecord = {
    ...workflowInstance,
    workflowInstanceId: "workflow_failed_debug",
    changeRequestId: "change_request_failed_debug",
    state: WORKFLOW_STATES.WAITING_REPAIR,
    status: WORKFLOW_STATUSES.WAITING_REPAIR,
    version: 3,
  };
  const outboxRow: IntegrationOutboxRecord = {
    outboxId: "outbox_failed_debug",
    tenantId: DEMO_IDS.tenantId,
    changeRequestId: "change_request_failed_debug",
    destination: "third_party_compensation_decision",
    operation: "submitCompensationChange",
    requestPayload: {
      salaryAmount: 145000,
      secretToken: "should_not_leave_debugger",
    },
    responsePayload: {
      status: "rejected",
    },
    status: INTEGRATION_OUTBOX_STATUSES.FAILED,
    attemptCount: 3,
    maxAttempts: 3,
    idempotencyKey: "idem_outbox_failed_debug",
    createdAt: nowIso(),
    updatedAt: nowIso(),
  };

  harness.repositories.store.workflowInstances.set(
    waitingRepairWorkflow.workflowInstanceId,
    waitingRepairWorkflow,
  );
  harness.repositories.store.integrationOutbox.set(outboxRow.outboxId, outboxRow);
  appendWorkflowLedgerEvent(harness, waitingRepairWorkflow, {
    eventType: LEDGER_EVENT_TYPES.EXTERNAL_WRITE_FAILED,
    payload: {
      nodeId: "vendor_compensation_decision",
      routeKey: "rejected",
      connectionId: "third_party_compensation_decision",
      operation: "submitCompensationChange",
      code: "COMP_VENDOR_REJECTED",
      safeMessage: "The compensation vendor rejected the payload.",
      salaryAmount: 145000,
      secretToken: "should_not_leave_debugger",
    },
  });
}

function appendWorkflowLedgerEvent(
  harness: TestHarness,
  workflowInstance: WorkflowInstanceRecord,
  input: {
    eventType: string;
    payload: Record<string, unknown>;
  },
): void {
  const appendResult = harness.repositories.ledger.append({
    tenantId: workflowInstance.tenantId,
    eventType: input.eventType,
    subjectType: "workflow_instance",
    subjectId: workflowInstance.workflowInstanceId,
    occurredAt: nowIso(),
    actorType: ACTOR_TYPES.HUMAN,
    actorId: DEMO_IDS.hrActorId,
    actorRole: "hr_admin",
    relationshipContext: {},
    workflowInstanceId: workflowInstance.workflowInstanceId,
    workflowDefinitionId: workflowInstance.workflowDefinitionId,
    workflowVersionId: workflowInstance.workflowVersionId,
    changeRequestId: workflowInstance.changeRequestId,
    correlationId: workflowInstance.correlationId,
    permissionSnapshot: {},
    aiVisibilitySnapshot: {},
    payload: input.payload,
  });

  if (!appendResult.ok) {
    throw new Error(appendResult.error.safeMessage);
  }
}

function createOtherTenantFamily(harness: TestHarness): void {
  const timestamp = nowIso();

  harness.repositories.store.workflowAdminFamilies.set("workflow_family_other_tenant", {
    workflowFamilyId: "workflow_family_other_tenant",
    tenantId: "tenant_other",
    environmentId: DEMO_IDS.environmentId,
    intent: "other.workflow",
    name: "Other tenant workflow",
    hcmDomain: "worker",
    ownerActorId: DEMO_IDS.hrActorId,
    status: "active",
    version: 1,
    createdAt: timestamp,
    updatedAt: timestamp,
    metadata: {},
  });
}

function requireWorkflowInstance(
  harness: TestHarness,
  workflowInstanceId: string,
): WorkflowInstanceRecord {
  const workflowInstance =
    harness.repositories.store.workflowInstances.get(workflowInstanceId);

  if (workflowInstance === undefined) {
    throw new Error(`Missing workflow instance ${workflowInstanceId}.`);
  }

  return workflowInstance;
}

function requireNode(
  workflowConfig: WorkflowConfig,
  nodeId: string,
): WorkflowGraphNodeConfig {
  const node = workflowConfig.graph?.nodes.find((candidate) => {
    return candidate.nodeId === nodeId;
  });

  if (node === undefined) {
    throw new Error(`Missing workflow node ${nodeId}.`);
  }

  return node;
}

function employeeDocument(harness: TestHarness): Record<string, unknown> {
  const employeeProjectionResult =
    harness.repositories.employeeProjections.findByEmployeeId(
      DEMO_IDS.tenantId,
      DEMO_IDS.employeeId,
    );

  if (!employeeProjectionResult.ok) {
    throw new Error(employeeProjectionResult.error.safeMessage);
  }

  return employeeProjectionResult.value.document;
}

function legalNameInput(): Record<string, unknown> {
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

function recordField(
  body: Record<string, unknown>,
  fieldName: string,
): Record<string, unknown> {
  const value = body[fieldName];

  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`Missing object field ${fieldName}.`);
  }

  return value as Record<string, unknown>;
}

function arrayField(
  body: Record<string, unknown>,
  fieldName: string,
): Record<string, unknown>[] {
  const value = body[fieldName];

  if (!Array.isArray(value)) {
    throw new Error(`Missing array field ${fieldName}.`);
  }

  return value.filter((item): item is Record<string, unknown> => {
    return typeof item === "object" && item !== null && !Array.isArray(item);
  });
}

function stringField(body: Record<string, unknown>, fieldName: string): string {
  const value = body[fieldName];

  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`Missing string field ${fieldName}.`);
  }

  return value;
}

function numberField(body: Record<string, unknown>, fieldName: string): number {
  const value = body[fieldName];

  if (typeof value !== "number") {
    throw new Error(`Missing number field ${fieldName}.`);
  }

  return value;
}

function expectAdminContract(body: Record<string, unknown>): void {
  expect(body["correlationId"]).toBe("corr_admin");
  expect(typeof body["generatedAt"]).toBe("string");
}

function startTestServer(server: Server): Promise<string> {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address() as AddressInfo;
      resolve(`http://127.0.0.1:${address.port}`);
    });
  });
}

function closeServer(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) {
        reject(error);
        return;
      }

      resolve();
    });
  });
}

function requireHarness(value: TestHarness | undefined): TestHarness {
  if (value === undefined) {
    throw new Error("Missing test harness.");
  }

  return value;
}
