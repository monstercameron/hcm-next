import {
  APPROVAL_TASK_STATUSES,
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  makeId,
  nowIso,
  type ApprovalGroupRecord,
  type ApprovalTaskRecord,
  type Repositories,
  type WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import { valueAtDotPath } from "../shared/json-fields.js";
import {
  evaluateApprovalGate,
  type ApprovalGateDecision,
  type ApprovalGateFailurePolicy,
  type ApprovalGatePassRule,
  type ApprovalGateTaskDecision,
  type ApprovalGateTaskStatus,
} from "../shared/approval-gate.js";
import type {
  WorkflowApprovalGateApproverResolverConfig,
  WorkflowApprovalGateConfig,
  WorkflowGraphNodeConfig,
} from "../shared/workflow-config.js";
import { createConfiguredApprovalTask } from "./records.js";

type ApprovalTaskSpec = {
  assigneeActorId: string;
  assigneeRole: string;
  approvalType: string;
  sequenceIndex: number;
  opensWithGate: boolean;
  weight?: number;
  isVetoHolder?: boolean;
  resolver: WorkflowApprovalGateApproverResolverConfig;
  assignmentMode: "actor" | "role";
};

export type OpenApprovalGateResult = {
  approvalGroup: ApprovalGroupRecord;
  approvalTasks: ApprovalTaskRecord[];
};

/**
 * Finds the approval gate node responsible for the workflow's state.
 */
export function findApprovalGateNodeForState(
  workflowInstance: WorkflowInstanceRecord,
  nodes: WorkflowGraphNodeConfig[] | undefined,
): WorkflowGraphNodeConfig | undefined {
  return nodes?.find((node) => {
    return (
      node.type === "approval_gate" &&
      (node.state === workflowInstance.state ||
        node.interaction ===
          stringFromRecord(workflowInstance.currentInteraction, "type"))
    );
  });
}

/**
 * Opens a configured approval gate and creates the initially visible tasks.
 */
export function openApprovalGate(input: {
  repositories: Repositories;
  tenantId: string;
  workflowInstance: WorkflowInstanceRecord;
  changeRequestId: string;
  gateNode: WorkflowGraphNodeConfig;
}): Result<OpenApprovalGateResult, AppError> {
  const gateConfig = input.gateNode.approvalGate;

  if (gateConfig === undefined) {
    return err(
      validationFailedError({
        nodeId: input.gateNode.nodeId,
        approvalGate: "missing",
      }),
    );
  }

  const taskSpecsResult = resolveApprovalTaskSpecs({
    repositories: input.repositories,
    tenantId: input.tenantId,
    workflowInstance: input.workflowInstance,
    gateConfig,
  });
  if (!taskSpecsResult.ok) {
    return taskSpecsResult;
  }

  const approvalGroup = createApprovalGroup({
    tenantId: input.tenantId,
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    changeRequestId: input.changeRequestId,
    gateNodeId: input.gateNode.nodeId,
    gateConfig,
    taskSpecs: taskSpecsResult.value,
  });
  const groupResult = input.repositories.approvalGroups.create(approvalGroup);
  if (!groupResult.ok) {
    return groupResult;
  }

  const initiallyOpenSpecs = initialOpenTaskSpecs(gateConfig, taskSpecsResult.value);
  const approvalTasks: ApprovalTaskRecord[] = [];

  for (const taskSpec of initiallyOpenSpecs) {
    const taskResult = input.repositories.approvals.create(
      createApprovalTaskFromSpec({
        taskSpec,
        tenantId: input.tenantId,
        workflowInstanceId: input.workflowInstance.workflowInstanceId,
        changeRequestId: input.changeRequestId,
        approvalGroupId: groupResult.value.approvalGroupId,
        gateNodeId: input.gateNode.nodeId,
      }),
    );
    if (!taskResult.ok) {
      return taskResult;
    }

    approvalTasks.push(taskResult.value);
  }

  return ok({
    approvalGroup: groupResult.value,
    approvalTasks,
  });
}

/**
 * Creates approval tasks for newly opened sequence indexes after a gate decision.
 */
export function openGateSequenceTasks(input: {
  repositories: Repositories;
  approvalGroup: ApprovalGroupRecord;
  workflowInstance: WorkflowInstanceRecord;
  openSequenceIndexes: number[];
}): Result<ApprovalTaskRecord[], AppError> {
  const taskSpecs = taskSpecsFromGroup(input.approvalGroup);
  const existingTasksResult = input.repositories.approvals.findByApprovalGroup(
    input.approvalGroup.approvalGroupId,
  );
  if (!existingTasksResult.ok) {
    return existingTasksResult;
  }

  const existingSequenceIndexes = new Set(
    existingTasksResult.value.map((task) => {
      return task.sequenceIndex ?? 0;
    }),
  );
  const tasksToCreate = taskSpecs.filter((taskSpec) => {
    return (
      input.openSequenceIndexes.includes(taskSpec.sequenceIndex) &&
      !existingSequenceIndexes.has(taskSpec.sequenceIndex)
    );
  });
  const createdTasks: ApprovalTaskRecord[] = [];

  for (const taskSpec of tasksToCreate) {
    const taskResult = input.repositories.approvals.create(
      createApprovalTaskFromSpec({
        taskSpec,
        tenantId: input.approvalGroup.tenantId,
        workflowInstanceId: input.workflowInstance.workflowInstanceId,
        changeRequestId: input.approvalGroup.changeRequestId ?? "",
        approvalGroupId: input.approvalGroup.approvalGroupId,
        gateNodeId: input.approvalGroup.gateNodeId,
      }),
    );
    if (!taskResult.ok) {
      return taskResult;
    }

    createdTasks.push(taskResult.value);
  }

  return ok(createdTasks);
}

/**
 * Evaluates a durable approval group after a task decision.
 */
export function evaluateApprovalGroup(input: {
  approvalGroup: ApprovalGroupRecord;
  approvalTasks: ApprovalTaskRecord[];
}): Result<ApprovalGateDecision, AppError> {
  const taskSpecs = taskSpecsFromGroup(input.approvalGroup);

  return evaluateApprovalGate({
    mode: input.approvalGroup.mode === "parallel" ? "parallel" : "sequential",
    passRule: input.approvalGroup.passRule as ApprovalGatePassRule,
    failurePolicy: input.approvalGroup.failurePolicy as ApprovalGateFailurePolicy,
    totalTaskCount: Math.max(taskSpecs.length, input.approvalTasks.length),
    tasks: input.approvalTasks.map((task) => {
      return {
        approvalTaskId: task.approvalTaskId,
        status: task.status as ApprovalGateTaskStatus,
        decision: task.decision as ApprovalGateTaskDecision | undefined,
        assigneeRole: task.assigneeRole,
        sequenceIndex: task.sequenceIndex,
        weight: task.weight,
        isVetoHolder: task.isVetoHolder,
        taskVersion: task.taskVersion,
      };
    }),
  });
}

export function taskSpecsFromGroup(
  approvalGroup: ApprovalGroupRecord,
): ApprovalTaskSpec[] {
  const taskSpecs = approvalGroup.metadata["taskSpecs"];

  if (!Array.isArray(taskSpecs)) {
    return [];
  }

  return taskSpecs.flatMap((taskSpec): ApprovalTaskSpec[] => {
    if (typeof taskSpec !== "object" || taskSpec === null) {
      return [];
    }

    const record = taskSpec as Record<string, unknown>;
    const assigneeActorId = stringValue(record["assigneeActorId"]);
    const assigneeRole = stringValue(record["assigneeRole"]);
    const approvalType = stringValue(record["approvalType"]);
    const sequenceIndex = numberValue(record["sequenceIndex"]);
    const resolver = record["resolver"];
    const assignmentMode = stringValue(record["assignmentMode"]);

    if (
      assigneeActorId === undefined ||
      assigneeRole === undefined ||
      approvalType === undefined ||
      sequenceIndex === undefined ||
      typeof resolver !== "object" ||
      resolver === null ||
      (assignmentMode !== "actor" && assignmentMode !== "role")
    ) {
      return [];
    }

    const weight = numberValue(record["weight"]);
    const isVetoHolder = booleanValue(record["isVetoHolder"]);

    return [
      {
        assigneeActorId,
        assigneeRole,
        approvalType,
        sequenceIndex,
        opensWithGate: booleanValue(record["opensWithGate"]) ?? false,
        ...(weight !== undefined ? { weight } : {}),
        ...(isVetoHolder !== undefined ? { isVetoHolder } : {}),
        resolver: resolver as WorkflowApprovalGateApproverResolverConfig,
        assignmentMode,
      },
    ];
  });
}

export function createApprovalTaskFromSpec(input: {
  taskSpec: ApprovalTaskSpec;
  tenantId: string;
  workflowInstanceId: string;
  changeRequestId: string;
  approvalGroupId: string;
  gateNodeId: string;
}): ApprovalTaskRecord {
  return createConfiguredApprovalTask({
    tenantId: input.tenantId,
    workflowInstanceId: input.workflowInstanceId,
    changeRequestId: input.changeRequestId,
    approvalGroupId: input.approvalGroupId,
    gateNodeId: input.gateNodeId,
    assigneeActorId: input.taskSpec.assigneeActorId,
    assigneeRole: input.taskSpec.assigneeRole,
    approvalType: input.taskSpec.approvalType,
    sequenceIndex: input.taskSpec.sequenceIndex,
    ...(input.taskSpec.weight !== undefined ? { weight: input.taskSpec.weight } : {}),
    ...(input.taskSpec.isVetoHolder !== undefined
      ? { isVetoHolder: input.taskSpec.isVetoHolder }
      : {}),
    resolver: input.taskSpec.resolver,
    assignmentMode: input.taskSpec.assignmentMode,
  });
}

export function cancelPendingGateTasks(input: {
  repositories: Repositories;
  approvalTasks: ApprovalTaskRecord[];
}): Result<ApprovalTaskRecord[], AppError> {
  const canceledTasks: ApprovalTaskRecord[] = [];

  for (const approvalTask of input.approvalTasks) {
    if (approvalTask.status !== APPROVAL_TASK_STATUSES.PENDING) {
      continue;
    }

    const taskResult = input.repositories.approvals.update({
      ...approvalTask,
      status: APPROVAL_TASK_STATUSES.CANCELED,
      decidedAt: nowIso(),
      taskVersion: (approvalTask.taskVersion ?? 1) + 1,
    });
    if (!taskResult.ok) {
      return taskResult;
    }

    canceledTasks.push(taskResult.value);
  }

  return ok(canceledTasks);
}

function resolveApprovalTaskSpecs(input: {
  repositories: Repositories;
  tenantId: string;
  workflowInstance: WorkflowInstanceRecord;
  gateConfig: WorkflowApprovalGateConfig;
}): Result<ApprovalTaskSpec[], AppError> {
  const taskSpecs: ApprovalTaskSpec[] = [];

  for (const [
    resolverIndex,
    resolver,
  ] of input.gateConfig.approverResolvers.entries()) {
    const resolvedSpecsResult = resolveApproverSpecsForResolver({
      repositories: input.repositories,
      tenantId: input.tenantId,
      workflowInstance: input.workflowInstance,
      resolver,
      resolverIndex,
    });
    if (!resolvedSpecsResult.ok) {
      return resolvedSpecsResult;
    }

    taskSpecs.push(...resolvedSpecsResult.value);
  }

  if (taskSpecs.length === 0) {
    return err(
      validationFailedError({
        gateId: input.gateConfig.gateId,
        approvers: "empty",
      }),
    );
  }

  return ok(taskSpecs);
}

function resolveApproverSpecsForResolver(input: {
  repositories: Repositories;
  tenantId: string;
  workflowInstance: WorkflowInstanceRecord;
  resolver: WorkflowApprovalGateApproverResolverConfig;
  resolverIndex: number;
}): Result<ApprovalTaskSpec[], AppError> {
  if (input.resolver.type === "workflow_field") {
    const fieldPath = input.resolver.fieldPath;
    const fieldValue =
      fieldPath === undefined
        ? undefined
        : valueAtDotPath(input.workflowInstance.context, fieldPath);
    const actorIds = stringValues(fieldValue);

    if (actorIds.length === 0) {
      return err(
        validationFailedError({
          resolverId: input.resolver.resolverId,
          fieldPath,
          actorIds,
        }),
      );
    }

    return ok(
      actorIds.map((actorId, actorIndex) =>
        approvalTaskSpec({
          resolver: input.resolver,
          assigneeActorId: actorId,
          assigneeRole: input.resolver.role ?? input.resolver.type,
          sequenceIndex: input.resolver.preserveOrder
            ? actorIndex
            : input.resolverIndex,
          assignmentMode: "actor",
        }),
      ),
    );
  }

  if (input.resolver.type === "actor" && input.resolver.actorId !== undefined) {
    return ok([
      approvalTaskSpec({
        resolver: input.resolver,
        assigneeActorId: input.resolver.actorId,
        assigneeRole: input.resolver.role ?? input.resolver.type,
        sequenceIndex: input.resolverIndex,
        assignmentMode: "actor",
      }),
    ]);
  }

  const configuredRole = input.resolver.role ?? input.resolver.type;
  const actorResult = input.repositories.actors.findFirstActiveByRole(
    input.tenantId,
    configuredRole,
  );

  return ok([
    approvalTaskSpec({
      resolver: input.resolver,
      assigneeActorId: actorResult.ok ? actorResult.value.actorId : "",
      assigneeRole: configuredRole,
      sequenceIndex: input.resolverIndex,
      assignmentMode: actorResult.ok ? "actor" : "role",
    }),
  ]);
}

function approvalTaskSpec(input: {
  resolver: WorkflowApprovalGateApproverResolverConfig;
  assigneeActorId: string;
  assigneeRole: string;
  sequenceIndex: number;
  assignmentMode: "actor" | "role";
}): ApprovalTaskSpec {
  return {
    assigneeActorId: input.assigneeActorId,
    assigneeRole: input.assigneeRole,
    approvalType: input.resolver.approvalType,
    sequenceIndex: input.sequenceIndex,
    opensWithGate: input.resolver.opensWithGate === true,
    ...(input.resolver.weight !== undefined ? { weight: input.resolver.weight } : {}),
    ...(input.resolver.isVetoHolder !== undefined
      ? { isVetoHolder: input.resolver.isVetoHolder }
      : {}),
    resolver: input.resolver,
    assignmentMode: input.assignmentMode,
  };
}

function createApprovalGroup(input: {
  tenantId: string;
  workflowInstanceId: string;
  changeRequestId: string;
  gateNodeId: string;
  gateConfig: WorkflowApprovalGateConfig;
  taskSpecs: ApprovalTaskSpec[];
}): ApprovalGroupRecord {
  const timestamp = nowIso();
  const failurePolicy = input.gateConfig.failurePolicies[0]?.type ?? "stop_workflow";

  return {
    approvalGroupId: makeId("apprgrp"),
    tenantId: input.tenantId,
    workflowInstanceId: input.workflowInstanceId,
    changeRequestId: input.changeRequestId,
    gateNodeId: input.gateNodeId,
    mode: input.gateConfig.mode,
    status: "active",
    passRule: input.gateConfig.passRule,
    failurePolicy,
    currentSequenceIndex: 0,
    openedAt: timestamp,
    createdAt: timestamp,
    updatedAt: timestamp,
    metadata: {
      gateId: input.gateConfig.gateId,
      taskSpecs: input.taskSpecs,
    },
  };
}

function initialOpenTaskSpecs(
  gateConfig: WorkflowApprovalGateConfig,
  taskSpecs: ApprovalTaskSpec[],
): ApprovalTaskSpec[] {
  if (gateConfig.mode === "sequential") {
    return taskSpecs.filter((taskSpec) => {
      return taskSpec.sequenceIndex === 0;
    });
  }

  const explicitlyOpenSpecs = taskSpecs.filter((taskSpec) => {
    return taskSpec.opensWithGate;
  });

  return explicitlyOpenSpecs.length > 0 ? explicitlyOpenSpecs : taskSpecs;
}

function stringValues(value: unknown): string[] {
  if (typeof value === "string" && value.trim().length > 0) {
    return [value.trim()];
  }

  if (!Array.isArray(value)) {
    return [];
  }

  return value.filter((item): item is string => {
    return typeof item === "string" && item.trim().length > 0;
  });
}

function stringFromRecord(
  record: Record<string, unknown>,
  key: string,
): string | undefined {
  return stringValue(record[key]);
}

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0
    ? value.trim()
    : undefined;
}

function numberValue(value: unknown): number | undefined {
  return typeof value === "number" ? value : undefined;
}

function booleanValue(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}
