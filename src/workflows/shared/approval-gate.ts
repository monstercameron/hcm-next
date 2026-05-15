import {
  err,
  ok,
  versionConflictError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";

export type ApprovalGateMode = "sequential" | "parallel";

export type ApprovalGateFailurePolicy =
  | "stop_workflow"
  | "send_to_repair"
  | "continue_until_threshold_impossible"
  | "require_all_responses"
  | "veto_only"
  | "escalate_on_timeout";

export type ApprovalGateRoleQuorum = {
  role: string;
  requiredApprovals: number;
};

export type ApprovalGatePassRule =
  | { type: "all_required" }
  | { type: "quorum"; requiredApprovals: number }
  | { type: "percentage"; requiredPercentage: number }
  | { type: "any_one" }
  | { type: "weighted"; requiredWeight: number }
  | { type: "role_quorum"; roleQuorums: ApprovalGateRoleQuorum[] }
  | { type: "composite"; rules: ApprovalGatePassRule[] };

export type ApprovalGateTaskDecision = "approved" | "rejected" | "more_info_requested";

export type ApprovalGateTaskStatus =
  | "pending"
  | "approved"
  | "rejected"
  | "delegated"
  | "expired"
  | "canceled"
  | "skipped"
  | "superseded"
  | "more_info_requested";

export type ApprovalGateTaskEvaluationInput = {
  approvalTaskId: string;
  status: ApprovalGateTaskStatus;
  decision?: ApprovalGateTaskDecision | undefined;
  assigneeRole?: string | undefined;
  sequenceIndex?: number | undefined;
  weight?: number | undefined;
  isVetoHolder?: boolean | undefined;
  taskVersion?: number | undefined;
  expectedTaskVersion?: number | undefined;
};

export type ApprovalGateEvaluationInput = {
  mode: ApprovalGateMode;
  passRule: ApprovalGatePassRule;
  failurePolicy?: ApprovalGateFailurePolicy | undefined;
  tasks: ApprovalGateTaskEvaluationInput[];
  totalTaskCount?: number | undefined;
  expectedTaskVersions?: Record<string, number> | undefined;
};

export type ApprovalGateDecisionOutcome =
  | "waiting"
  | "advance_sequence"
  | "passed"
  | "failed"
  | "repair";

export type ApprovalGateRoleProgress = {
  role: string;
  requiredApprovals: number;
  approvedCount: number;
  pendingCount: number;
  rejectedCount: number;
};

export type ApprovalGateProgress = {
  totalTaskCount: number;
  approvedCount: number;
  rejectedCount: number;
  pendingCount: number;
  ignoredCount: number;
  approvalWeight: number;
  pendingWeight: number;
  requiredApprovals?: number | undefined;
  requiredWeight?: number | undefined;
  requiredPercentage?: number | undefined;
  roleProgress?: ApprovalGateRoleProgress[] | undefined;
};

export type ApprovalGateDecision = {
  outcome: ApprovalGateDecisionOutcome;
  reason: string;
  mode: ApprovalGateMode;
  failurePolicy: ApprovalGateFailurePolicy;
  passRuleType: ApprovalGatePassRule["type"];
  progress: ApprovalGateProgress;
  openSequenceIndexes: number[];
  cancelPendingTaskIds: string[];
  currentSequenceIndex?: number | undefined;
  failedRuleTypes?: ApprovalGatePassRule["type"][] | undefined;
};

type NormalizedTaskDecision =
  | "approved"
  | "rejected"
  | "more_info_requested"
  | "pending"
  | "ignored";

type CountedTask = ApprovalGateTaskEvaluationInput & {
  normalizedDecision: NormalizedTaskDecision;
  sequenceIndex: number;
  weight: number;
};

type RuleEvaluation = {
  outcome: "passed" | "failed" | "waiting";
  reason: string;
  progressPatch?: Partial<ApprovalGateProgress> | undefined;
  failedRuleTypes?: ApprovalGatePassRule["type"][] | undefined;
};

const DEFAULT_FAILURE_POLICY: ApprovalGateFailurePolicy =
  "continue_until_threshold_impossible";

/**
 * Evaluates a snapshot of approval tasks against a gate pass rule without side effects.
 */
export function evaluateApprovalGate(
  input: ApprovalGateEvaluationInput,
): Result<ApprovalGateDecision, AppError> {
  const versionResult = validateTaskVersions(input);

  if (!versionResult.ok) {
    return versionResult;
  }

  const tasks = normalizeTasks(input.tasks);
  const progress = buildProgress(tasks, input.totalTaskCount);

  if (input.mode === "sequential") {
    return ok(evaluateSequentialGate(input, tasks, progress));
  }

  return ok(evaluateParallelGate(input, tasks, progress));
}

function validateTaskVersions(
  input: ApprovalGateEvaluationInput,
): Result<void, AppError> {
  for (const task of input.tasks) {
    const expectedTaskVersion =
      task.expectedTaskVersion ?? input.expectedTaskVersions?.[task.approvalTaskId];

    if (expectedTaskVersion !== undefined && task.taskVersion !== expectedTaskVersion) {
      return err(
        versionConflictError({
          approvalTaskId: task.approvalTaskId,
          expectedTaskVersion,
          actualTaskVersion: task.taskVersion ?? null,
        }),
      );
    }
  }

  return ok(undefined);
}

function normalizeTasks(tasks: ApprovalGateTaskEvaluationInput[]): CountedTask[] {
  const tasksById = new Map<string, CountedTask>();

  for (const task of tasks) {
    const normalizedTask = normalizeTask(task);
    const existingTask = tasksById.get(normalizedTask.approvalTaskId);

    if (existingTask === undefined || shouldReplaceTask(existingTask, normalizedTask)) {
      tasksById.set(normalizedTask.approvalTaskId, normalizedTask);
    }
  }

  return [...tasksById.values()];
}

function normalizeTask(task: ApprovalGateTaskEvaluationInput): CountedTask {
  const sequenceIndex = task.sequenceIndex ?? 0;
  const weight = task.weight ?? 1;

  return {
    ...task,
    sequenceIndex,
    weight,
    normalizedDecision: normalizedDecisionForTask(task),
  };
}

function shouldReplaceTask(existingTask: CountedTask, nextTask: CountedTask): boolean {
  const existingVersion = existingTask.taskVersion ?? 0;
  const nextVersion = nextTask.taskVersion ?? 0;

  if (nextVersion !== existingVersion) {
    return nextVersion > existingVersion;
  }

  return (
    existingTask.normalizedDecision === "pending" &&
    nextTask.normalizedDecision !== "pending"
  );
}

function normalizedDecisionForTask(
  task: ApprovalGateTaskEvaluationInput,
): NormalizedTaskDecision {
  if (task.decision === "more_info_requested") {
    return "more_info_requested";
  }

  if (task.status === "canceled" || task.status === "superseded") {
    return "ignored";
  }

  if (task.status === "skipped" || task.status === "delegated") {
    return "ignored";
  }

  if (task.decision === "approved" || task.status === "approved") {
    return "approved";
  }

  if (task.decision === "rejected" || task.status === "rejected") {
    return "rejected";
  }

  if (
    task.decision === "more_info_requested" ||
    task.status === "more_info_requested"
  ) {
    return "more_info_requested";
  }

  if (task.status === "expired") {
    return "rejected";
  }

  return "pending";
}

function buildProgress(
  tasks: CountedTask[],
  totalTaskCount: number | undefined,
): ApprovalGateProgress {
  const countableTasks = countableTasksFrom(tasks);
  const approvedTasks = tasksWithDecision(countableTasks, "approved");
  const rejectedTasks = tasksWithDecision(countableTasks, "rejected");
  const pendingTasks = tasksWithDecision(countableTasks, "pending");
  const ignoredCount = tasks.filter((task) => {
    return task.normalizedDecision === "ignored";
  }).length;

  return {
    totalTaskCount: Math.max(
      totalTaskCount ?? countableTasks.length,
      countableTasks.length,
    ),
    approvedCount: approvedTasks.length,
    rejectedCount: rejectedTasks.length,
    pendingCount: pendingTasks.length,
    ignoredCount,
    approvalWeight: sumWeight(approvedTasks),
    pendingWeight: sumWeight(pendingTasks),
  };
}

function evaluateSequentialGate(
  input: ApprovalGateEvaluationInput,
  tasks: CountedTask[],
  progress: ApprovalGateProgress,
): ApprovalGateDecision {
  const failurePolicy = input.failurePolicy ?? DEFAULT_FAILURE_POLICY;
  const totalSequenceCount = Math.max(
    input.totalTaskCount ?? highestSequenceIndex(tasks) + 1,
    tasks.length === 0 ? 1 : highestSequenceIndex(tasks) + 1,
  );
  const moreInfoTask = firstTaskWithDecision(tasks, "more_info_requested");
  const rejectedTask = firstTaskWithDecision(tasks, "rejected");
  const pendingTaskIds = pendingTaskIdsFrom(tasks);

  if (moreInfoTask !== undefined) {
    return buildDecision({
      input,
      progress,
      failurePolicy,
      outcome: "repair",
      reason: "more_info_requested",
      cancelPendingTaskIds: pendingTaskIds,
      currentSequenceIndex: moreInfoTask.sequenceIndex,
    });
  }

  if (rejectedTask !== undefined) {
    return buildFailureDecision({
      input,
      progress,
      failurePolicy,
      reason: "sequential_task_rejected",
      cancelPendingTaskIds: pendingTaskIds,
      currentSequenceIndex: rejectedTask.sequenceIndex,
    });
  }

  const firstIncompleteSequenceIndex = firstIncompleteSequence(
    tasks,
    totalSequenceCount,
  );

  if (firstIncompleteSequenceIndex === undefined) {
    return buildDecision({
      input,
      progress,
      failurePolicy,
      outcome: "passed",
      reason: "all_sequence_tasks_approved",
      cancelPendingTaskIds: [],
    });
  }

  const currentTask = tasks.find((task) => {
    return (
      task.sequenceIndex === firstIncompleteSequenceIndex &&
      task.normalizedDecision !== "ignored"
    );
  });

  if (currentTask?.normalizedDecision === "pending") {
    return buildDecision({
      input,
      progress,
      failurePolicy,
      outcome: "waiting",
      reason: "current_sequence_task_pending",
      currentSequenceIndex: firstIncompleteSequenceIndex,
    });
  }

  const hasApprovedPreviousSequence =
    firstIncompleteSequenceIndex > 0 &&
    tasks.some((task) => {
      return (
        task.sequenceIndex === firstIncompleteSequenceIndex - 1 &&
        task.normalizedDecision === "approved"
      );
    });

  return buildDecision({
    input,
    progress,
    failurePolicy,
    outcome: hasApprovedPreviousSequence ? "advance_sequence" : "waiting",
    reason: hasApprovedPreviousSequence ? "open_next_sequence_task" : "open_first_task",
    openSequenceIndexes: [firstIncompleteSequenceIndex],
    currentSequenceIndex: firstIncompleteSequenceIndex,
  });
}

function evaluateParallelGate(
  input: ApprovalGateEvaluationInput,
  tasks: CountedTask[],
  progress: ApprovalGateProgress,
): ApprovalGateDecision {
  const failurePolicy = input.failurePolicy ?? DEFAULT_FAILURE_POLICY;
  const moreInfoTask = firstTaskWithDecision(tasks, "more_info_requested");
  const vetoRejectedTask = tasks.find((task) => {
    return task.isVetoHolder === true && task.normalizedDecision === "rejected";
  });
  const pendingTaskIds = pendingTaskIdsFrom(tasks);

  if (moreInfoTask !== undefined) {
    return buildDecision({
      input,
      progress,
      failurePolicy,
      outcome: "repair",
      reason: "more_info_requested",
      cancelPendingTaskIds: pendingTaskIds,
    });
  }

  if (vetoRejectedTask !== undefined) {
    return buildDecision({
      input,
      progress,
      failurePolicy,
      outcome: "failed",
      reason: "veto_rejected",
      cancelPendingTaskIds: pendingTaskIds,
    });
  }

  const ruleResult = evaluatePassRule(input.passRule, tasks, progress);
  const mergedProgress = {
    ...progress,
    ...ruleResult.progressPatch,
  };

  if (ruleResult.outcome === "passed") {
    if (failurePolicy === "require_all_responses" && progress.pendingCount > 0) {
      return buildDecision({
        input,
        progress: mergedProgress,
        failurePolicy,
        outcome: "waiting",
        reason: "awaiting_all_responses",
      });
    }

    return buildDecision({
      input,
      progress: mergedProgress,
      failurePolicy,
      outcome: "passed",
      reason: ruleResult.reason,
      cancelPendingTaskIds: pendingTaskIds,
    });
  }

  if (ruleResult.outcome === "failed") {
    if (failurePolicy === "veto_only") {
      return buildDecision({
        input,
        progress: mergedProgress,
        failurePolicy,
        outcome: "waiting",
        reason: "waiting_for_veto_or_pass",
      });
    }

    return buildFailureDecision({
      input,
      progress: mergedProgress,
      failurePolicy,
      reason: ruleResult.reason,
      cancelPendingTaskIds: pendingTaskIds,
      failedRuleTypes: ruleResult.failedRuleTypes,
    });
  }

  return buildDecision({
    input,
    progress: mergedProgress,
    failurePolicy,
    outcome: "waiting",
    reason: ruleResult.reason,
    failedRuleTypes: ruleResult.failedRuleTypes,
  });
}

function evaluatePassRule(
  passRule: ApprovalGatePassRule,
  tasks: CountedTask[],
  progress: ApprovalGateProgress,
): RuleEvaluation {
  switch (passRule.type) {
    case "all_required":
      return evaluateAllRequiredRule(progress);
    case "quorum":
      return evaluateQuorumRule(progress, passRule.requiredApprovals);
    case "percentage":
      return evaluatePercentageRule(progress, passRule.requiredPercentage);
    case "any_one":
      return evaluateQuorumRule(progress, 1);
    case "weighted":
      return evaluateWeightedRule(progress, passRule.requiredWeight);
    case "role_quorum":
      return evaluateRoleQuorumRule(tasks, passRule.roleQuorums);
    case "composite":
      return evaluateCompositeRule(passRule.rules, tasks, progress);
    default:
      return {
        outcome: "failed",
        reason: "unknown_pass_rule",
      };
  }
}

function evaluateAllRequiredRule(progress: ApprovalGateProgress): RuleEvaluation {
  if (progress.approvedCount >= progress.totalTaskCount) {
    return {
      outcome: "passed",
      reason: "all_required_approved",
      progressPatch: { requiredApprovals: progress.totalTaskCount },
    };
  }

  if (progress.rejectedCount > 0) {
    return {
      outcome: "failed",
      reason: "all_required_impossible",
      progressPatch: { requiredApprovals: progress.totalTaskCount },
      failedRuleTypes: ["all_required"],
    };
  }

  return {
    outcome: "waiting",
    reason: "all_required_waiting",
    progressPatch: { requiredApprovals: progress.totalTaskCount },
  };
}

function evaluateQuorumRule(
  progress: ApprovalGateProgress,
  requiredApprovals: number,
): RuleEvaluation {
  const possibleApprovalCount = progress.approvedCount + progress.pendingCount;

  if (requiredApprovals <= 0) {
    return {
      outcome: "failed",
      reason: "invalid_required_approvals",
      progressPatch: { requiredApprovals },
      failedRuleTypes: ["quorum"],
    };
  }

  if (progress.approvedCount >= requiredApprovals) {
    return {
      outcome: "passed",
      reason: "quorum_reached",
      progressPatch: { requiredApprovals },
    };
  }

  if (possibleApprovalCount < requiredApprovals) {
    return {
      outcome: "failed",
      reason: "quorum_impossible",
      progressPatch: { requiredApprovals },
      failedRuleTypes: ["quorum"],
    };
  }

  return {
    outcome: "waiting",
    reason: "quorum_still_possible",
    progressPatch: { requiredApprovals },
  };
}

function evaluatePercentageRule(
  progress: ApprovalGateProgress,
  requiredPercentage: number,
): RuleEvaluation {
  const normalizedPercentage =
    requiredPercentage > 1 ? requiredPercentage / 100 : requiredPercentage;
  const requiredApprovals = Math.ceil(progress.totalTaskCount * normalizedPercentage);
  const quorumResult = evaluateQuorumRule(progress, requiredApprovals);

  return {
    ...quorumResult,
    reason:
      quorumResult.outcome === "passed" ? "percentage_reached" : quorumResult.reason,
    progressPatch: {
      ...quorumResult.progressPatch,
      requiredPercentage: normalizedPercentage,
    },
    failedRuleTypes:
      quorumResult.outcome === "failed" ? ["percentage"] : quorumResult.failedRuleTypes,
  };
}

function evaluateWeightedRule(
  progress: ApprovalGateProgress,
  requiredWeight: number,
): RuleEvaluation {
  const possibleApprovalWeight = progress.approvalWeight + progress.pendingWeight;

  if (requiredWeight <= 0) {
    return {
      outcome: "failed",
      reason: "invalid_required_weight",
      progressPatch: { requiredWeight },
      failedRuleTypes: ["weighted"],
    };
  }

  if (progress.approvalWeight >= requiredWeight) {
    return {
      outcome: "passed",
      reason: "weight_threshold_reached",
      progressPatch: { requiredWeight },
    };
  }

  if (possibleApprovalWeight < requiredWeight) {
    return {
      outcome: "failed",
      reason: "weight_threshold_impossible",
      progressPatch: { requiredWeight },
      failedRuleTypes: ["weighted"],
    };
  }

  return {
    outcome: "waiting",
    reason: "weight_threshold_still_possible",
    progressPatch: { requiredWeight },
  };
}

function evaluateRoleQuorumRule(
  tasks: CountedTask[],
  roleQuorums: ApprovalGateRoleQuorum[],
): RuleEvaluation {
  const roleProgress = roleQuorums.map((roleQuorum) => {
    const roleTasks = countableTasksFrom(tasks).filter((task) => {
      return task.assigneeRole === roleQuorum.role;
    });

    return {
      role: roleQuorum.role,
      requiredApprovals: roleQuorum.requiredApprovals,
      approvedCount: tasksWithDecision(roleTasks, "approved").length,
      pendingCount: tasksWithDecision(roleTasks, "pending").length,
      rejectedCount: tasksWithDecision(roleTasks, "rejected").length,
    };
  });

  const impossibleRole = roleProgress.find((role) => {
    return role.approvedCount + role.pendingCount < role.requiredApprovals;
  });

  if (impossibleRole !== undefined) {
    return {
      outcome: "failed",
      reason: "role_quorum_impossible",
      progressPatch: { roleProgress },
      failedRuleTypes: ["role_quorum"],
    };
  }

  const allRolesPassed = roleProgress.every((role) => {
    return role.approvedCount >= role.requiredApprovals;
  });

  if (allRolesPassed) {
    return {
      outcome: "passed",
      reason: "role_quorum_reached",
      progressPatch: { roleProgress },
    };
  }

  return {
    outcome: "waiting",
    reason: "role_quorum_still_possible",
    progressPatch: { roleProgress },
  };
}

function evaluateCompositeRule(
  rules: ApprovalGatePassRule[],
  tasks: CountedTask[],
  progress: ApprovalGateProgress,
): RuleEvaluation {
  if (rules.length === 0) {
    return {
      outcome: "failed",
      reason: "empty_composite_rule",
      failedRuleTypes: ["composite"],
    };
  }

  const childResults = rules.map((rule) => {
    return evaluatePassRule(rule, tasks, progress);
  });
  const failedChild = childResults.find((result) => {
    return result.outcome === "failed";
  });

  if (failedChild !== undefined) {
    return {
      outcome: "failed",
      reason: "composite_rule_failed",
      failedRuleTypes: ["composite", ...(failedChild.failedRuleTypes ?? [])],
    };
  }

  const allChildrenPassed = childResults.every((result) => {
    return result.outcome === "passed";
  });

  return {
    outcome: allChildrenPassed ? "passed" : "waiting",
    reason: allChildrenPassed ? "composite_rule_passed" : "composite_rule_waiting",
  };
}

function buildFailureDecision(input: {
  input: ApprovalGateEvaluationInput;
  progress: ApprovalGateProgress;
  failurePolicy: ApprovalGateFailurePolicy;
  reason: string;
  cancelPendingTaskIds: string[];
  currentSequenceIndex?: number | undefined;
  failedRuleTypes?: ApprovalGatePassRule["type"][] | undefined;
}): ApprovalGateDecision {
  if (input.failurePolicy === "send_to_repair") {
    return buildDecision({
      ...input,
      outcome: "repair",
    });
  }

  if (
    input.failurePolicy === "require_all_responses" &&
    input.progress.pendingCount > 0
  ) {
    return buildDecision({
      ...input,
      outcome: "waiting",
      reason: "awaiting_all_responses",
      cancelPendingTaskIds: [],
    });
  }

  return buildDecision({
    ...input,
    outcome: "failed",
  });
}

function buildDecision(input: {
  input: ApprovalGateEvaluationInput;
  progress: ApprovalGateProgress;
  failurePolicy: ApprovalGateFailurePolicy;
  outcome: ApprovalGateDecisionOutcome;
  reason: string;
  openSequenceIndexes?: number[] | undefined;
  cancelPendingTaskIds?: string[] | undefined;
  currentSequenceIndex?: number | undefined;
  failedRuleTypes?: ApprovalGatePassRule["type"][] | undefined;
}): ApprovalGateDecision {
  return {
    outcome: input.outcome,
    reason: input.reason,
    mode: input.input.mode,
    failurePolicy: input.failurePolicy,
    passRuleType: input.input.passRule.type,
    progress: input.progress,
    openSequenceIndexes: input.openSequenceIndexes ?? [],
    cancelPendingTaskIds: input.cancelPendingTaskIds ?? [],
    currentSequenceIndex: input.currentSequenceIndex,
    failedRuleTypes: input.failedRuleTypes,
  };
}

function firstIncompleteSequence(
  tasks: CountedTask[],
  totalSequenceCount: number,
): number | undefined {
  for (let sequenceIndex = 0; sequenceIndex < totalSequenceCount; sequenceIndex += 1) {
    const sequenceApproved = tasks.some((task) => {
      return (
        task.sequenceIndex === sequenceIndex && task.normalizedDecision === "approved"
      );
    });

    if (!sequenceApproved) {
      return sequenceIndex;
    }
  }

  return undefined;
}

function firstTaskWithDecision(
  tasks: CountedTask[],
  decision: NormalizedTaskDecision,
): CountedTask | undefined {
  return tasks.find((task) => {
    return task.normalizedDecision === decision;
  });
}

function tasksWithDecision(
  tasks: CountedTask[],
  decision: NormalizedTaskDecision,
): CountedTask[] {
  return tasks.filter((task) => {
    return task.normalizedDecision === decision;
  });
}

function countableTasksFrom(tasks: CountedTask[]): CountedTask[] {
  return tasks.filter((task) => {
    return task.normalizedDecision !== "ignored";
  });
}

function pendingTaskIdsFrom(tasks: CountedTask[]): string[] {
  return tasksWithDecision(tasks, "pending").map((task) => {
    return task.approvalTaskId;
  });
}

function highestSequenceIndex(tasks: CountedTask[]): number {
  return tasks.reduce((highestIndex, task) => {
    return Math.max(highestIndex, task.sequenceIndex);
  }, -1);
}

function sumWeight(tasks: CountedTask[]): number {
  return tasks.reduce((total, task) => {
    return total + task.weight;
  }, 0);
}
