import { ok, type AppError, type Result } from "@hcm-next/foundation";
import type { ActorRecord } from "@hcm-next/data-store";
import type {
  WorkflowApprovalGateApproverResolverConfig,
  WorkflowApprovalGateConfig,
  WorkflowApprovalGatePassRuleConfig,
  WorkflowGraphConfig,
} from "../../shared/workflow-config.js";
import {
  adminError,
  adminWarning,
  workflowAdminValidationReport,
  type WorkflowAdminIssue,
  type WorkflowAdminValidationReport,
} from "./admin-issues.js";

export type ApprovalGateResolverFixture = {
  actors: ActorRecord[];
  workflowContext?: Record<string, unknown>;
};

export type ApprovalGateResolvedApproverPreview = {
  resolverId: string;
  actorIds: string[];
  role?: string;
  assignmentMode: "actor" | "role" | "unresolved";
  isVetoHolder: boolean;
  weight: number;
};

export type ApprovalGateValidationResult = WorkflowAdminValidationReport & {
  gateId: string;
  resolvedApprovers: ApprovalGateResolvedApproverPreview[];
};

const supportedGateModes = new Set(["sequential", "parallel"]);
const supportedResolverTypes = new Set([
  "actor",
  "role",
  "relationship",
  "manager_chain",
  "department_lead",
  "cost_center_owner",
  "org_unit",
  "seniority_level",
  "workflow_field",
]);
const supportedFailurePolicyTypes = new Set([
  "stop_workflow",
  "send_to_repair",
  "continue_until_threshold_impossible",
  "require_all_responses",
  "veto_only",
  "escalate_on_timeout",
]);

/**
 * Validates an approval gate config before a workflow draft can be published.
 */
export function validateApprovalGateAdminConfig(input: {
  gateConfig: WorkflowApprovalGateConfig;
  graph?: WorkflowGraphConfig;
  resolverFixture?: ApprovalGateResolverFixture;
  path?: string;
}): Result<ApprovalGateValidationResult, AppError> {
  const gatePath = input.path ?? `approvalGates.${input.gateConfig.gateId}`;
  const issues: WorkflowAdminIssue[] = [];
  const resolvedApprovers = input.gateConfig.approverResolvers.map((resolver) =>
    resolveApproverPreview(resolver, input.resolverFixture),
  );

  validateBasicGateShape(input.gateConfig, gatePath, issues);
  validateApproverResolvers(input.gateConfig.approverResolvers, gatePath, issues);
  validatePassRule({
    passRule: input.gateConfig.passRule,
    resolverCount: input.gateConfig.approverResolvers.length,
    path: `${gatePath}.passRule`,
    issues,
  });
  validateFailurePolicies({
    gateConfig: input.gateConfig,
    ...(input.graph !== undefined ? { graph: input.graph } : {}),
    path: `${gatePath}.failurePolicies`,
    issues,
  });
  validateRequestMoreInfoPolicy({
    gateConfig: input.gateConfig,
    ...(input.graph !== undefined ? { graph: input.graph } : {}),
    path: `${gatePath}.requestMoreInfoPolicy`,
    issues,
  });
  validateTaskPolicies({
    gateConfig: input.gateConfig,
    path: gatePath,
    issues,
  });
  validateResolvedApprovers({
    resolvedApprovers,
    gatePath,
    issues,
  });

  return ok({
    ...workflowAdminValidationReport(issues),
    gateId: input.gateConfig.gateId,
    resolvedApprovers,
  });
}

function validateBasicGateShape(
  gateConfig: WorkflowApprovalGateConfig,
  gatePath: string,
  issues: WorkflowAdminIssue[],
): void {
  if (!supportedGateModes.has(gateConfig.mode)) {
    issues.push(
      adminError({
        code: "approval_gate.mode_unknown",
        path: `${gatePath}.mode`,
        message: `Approval gate mode ${gateConfig.mode} is not supported.`,
        suggestedRepair: "Use sequential or parallel.",
      }),
    );
  }

  if (gateConfig.approverResolvers.length === 0) {
    issues.push(
      adminError({
        code: "approval_gate.resolvers_empty",
        path: `${gatePath}.approverResolvers`,
        message: "Approval gate needs at least one approver resolver.",
        suggestedRepair: "Add a role, actor, relationship, or workflow-field resolver.",
      }),
    );
  }

  if (gateConfig.taskVersionRequired !== true) {
    issues.push(
      adminWarning({
        code: "approval_gate.task_version_not_required",
        path: `${gatePath}.taskVersionRequired`,
        message: "Approval tasks should require task-version checks for admin safety.",
        suggestedRepair: "Set taskVersionRequired to true.",
      }),
    );
  }
}

function validateApproverResolvers(
  resolvers: WorkflowApprovalGateApproverResolverConfig[],
  gatePath: string,
  issues: WorkflowAdminIssue[],
): void {
  const resolverIds = new Set<string>();
  const mandatoryActorIds = new Map<string, string>();

  resolvers.forEach((resolver, resolverIndex) => {
    const resolverPath = `${gatePath}.approverResolvers.${resolverIndex}`;

    if (resolverIds.has(resolver.resolverId)) {
      issues.push(
        adminError({
          code: "approval_gate.resolver_id_duplicate",
          path: `${resolverPath}.resolverId`,
          message: `Resolver ID ${resolver.resolverId} is duplicated.`,
          suggestedRepair: "Use one stable resolver ID per approver resolver.",
        }),
      );
    }
    resolverIds.add(resolver.resolverId);

    if (!supportedResolverTypes.has(resolver.type)) {
      issues.push(
        adminError({
          code: "approval_gate.resolver_type_unknown",
          path: `${resolverPath}.type`,
          message: `Approval resolver type ${resolver.type} is not supported.`,
          suggestedRepair: "Use a supported resolver type.",
        }),
      );
    }

    validateResolverRequiredFields(resolver, resolverPath, issues);

    if (resolver.actorId !== undefined) {
      const duplicateResolverId = mandatoryActorIds.get(resolver.actorId);

      if (duplicateResolverId !== undefined) {
        issues.push(
          adminWarning({
            code: "approval_gate.duplicate_mandatory_actor",
            path: `${resolverPath}.actorId`,
            message: `Actor ${resolver.actorId} appears in more than one resolver.`,
            suggestedRepair:
              "Keep duplicate mandatory actors only when the gate intentionally needs repeat approval.",
            details: { duplicateResolverId },
          }),
        );
      }

      mandatoryActorIds.set(resolver.actorId, resolver.resolverId);
    }

    if (resolver.weight !== undefined && resolver.weight <= 0) {
      issues.push(
        adminError({
          code: "approval_gate.resolver_weight_invalid",
          path: `${resolverPath}.weight`,
          message: "Approver resolver weight must be greater than zero.",
          suggestedRepair: "Set a positive weight or remove the field.",
        }),
      );
    }
  });
}

function validateResolverRequiredFields(
  resolver: WorkflowApprovalGateApproverResolverConfig,
  resolverPath: string,
  issues: WorkflowAdminIssue[],
): void {
  const requiredFieldByResolverType: Partial<
    Record<
      WorkflowApprovalGateApproverResolverConfig["type"],
      keyof WorkflowApprovalGateApproverResolverConfig
    >
  > = {
    actor: "actorId",
    role: "role",
    relationship: "subjectPath",
    manager_chain: "subjectPath",
    department_lead: "departmentPath",
    cost_center_owner: "costCenterPath",
    org_unit: "departmentPath",
    seniority_level: "seniorityLevelPath",
    workflow_field: "fieldPath",
  };
  const requiredField = requiredFieldByResolverType[resolver.type];

  if (requiredField !== undefined && blankString(resolver[requiredField])) {
    issues.push(
      adminError({
        code: "approval_gate.resolver_required_field_missing",
        path: `${resolverPath}.${String(requiredField)}`,
        message: `Resolver ${resolver.resolverId} is missing ${String(requiredField)}.`,
        suggestedRepair: `Configure ${String(requiredField)} for this resolver.`,
      }),
    );
  }

  if (blankString(resolver.permission)) {
    issues.push(
      adminError({
        code: "approval_gate.resolver_permission_missing",
        path: `${resolverPath}.permission`,
        message: `Resolver ${resolver.resolverId} needs an approval permission.`,
        suggestedRepair: "Set the permission required to complete this approval task.",
      }),
    );
  }
}

function validateRequestMoreInfoPolicy(input: {
  gateConfig: WorkflowApprovalGateConfig;
  graph?: WorkflowGraphConfig;
  path: string;
  issues: WorkflowAdminIssue[];
}): void {
  const policy = input.gateConfig.requestMoreInfoPolicy;
  if (policy === undefined || !policy.enabled) {
    return;
  }

  if (
    policy.nextNodeId === undefined &&
    policy.nextState === undefined &&
    policy.nextInteraction === undefined
  ) {
    input.issues.push(
      adminWarning({
        code: "approval_gate.request_more_info_route_missing",
        path: input.path,
        message:
          "Request-more-info policy is enabled without an explicit route or state.",
        suggestedRepair:
          "Set nextNodeId, nextState, or nextInteraction for request-more-info.",
      }),
    );
  }

  if (policy.nextNodeId !== undefined && input.graph !== undefined) {
    const nodeIds = new Set(input.graph.nodes.map((node) => node.nodeId));
    if (!nodeIds.has(policy.nextNodeId)) {
      input.issues.push(
        adminError({
          code: "approval_gate.request_more_info_next_node_missing",
          path: `${input.path}.nextNodeId`,
          message: `Request-more-info policy references missing node ${policy.nextNodeId}.`,
          suggestedRepair: "Point request-more-info to an existing node.",
        }),
      );
    }
  }
}

function validateTaskPolicies(input: {
  gateConfig: WorkflowApprovalGateConfig;
  path: string;
  issues: WorkflowAdminIssue[];
}): void {
  const staleTaskPolicy = input.gateConfig.staleTaskPolicy;
  const taskExpirationPolicy = input.gateConfig.taskExpirationPolicy;
  const delegationPolicy = input.gateConfig.delegationPolicy;
  const resolverIds = new Set(
    input.gateConfig.approverResolvers.map((resolver) => resolver.resolverId),
  );

  if (staleTaskPolicy !== undefined && blankString(staleTaskPolicy.after)) {
    input.issues.push(
      adminError({
        code: "approval_gate.stale_task_policy_after_missing",
        path: `${input.path}.staleTaskPolicy.after`,
        message: "Stale task policy needs an after duration.",
        suggestedRepair: "Set a duration such as PT48H.",
      }),
    );
  }

  if (
    staleTaskPolicy?.action === "escalate" &&
    blankString(staleTaskPolicy.escalationResolverId)
  ) {
    input.issues.push(
      adminError({
        code: "approval_gate.stale_task_escalation_missing",
        path: `${input.path}.staleTaskPolicy.escalationResolverId`,
        message: "Escalating stale task policies need an escalation resolver.",
        suggestedRepair: "Set escalationResolverId to one configured resolver.",
      }),
    );
  }

  if (
    staleTaskPolicy?.escalationResolverId !== undefined &&
    !resolverIds.has(staleTaskPolicy.escalationResolverId)
  ) {
    input.issues.push(
      adminError({
        code: "approval_gate.stale_task_escalation_resolver_missing",
        path: `${input.path}.staleTaskPolicy.escalationResolverId`,
        message: `Stale task policy references missing resolver ${staleTaskPolicy.escalationResolverId}.`,
        suggestedRepair: "Use a resolver ID from approverResolvers.",
      }),
    );
  }

  if (
    taskExpirationPolicy !== undefined &&
    blankString(taskExpirationPolicy.expiresAfter)
  ) {
    input.issues.push(
      adminError({
        code: "approval_gate.task_expiration_after_missing",
        path: `${input.path}.taskExpirationPolicy.expiresAfter`,
        message: "Task expiration policy needs an expiresAfter duration.",
        suggestedRepair: "Set a duration such as PT72H.",
      }),
    );
  }

  if (delegationPolicy?.enabled === true && delegationPolicy.requiresAudit !== true) {
    input.issues.push(
      adminWarning({
        code: "approval_gate.delegation_audit_not_required",
        path: `${input.path}.delegationPolicy.requiresAudit`,
        message: "Delegation should require audit for enterprise approval gates.",
        suggestedRepair: "Set requiresAudit to true.",
      }),
    );
  }
}

function validatePassRule(input: {
  passRule: WorkflowApprovalGatePassRuleConfig;
  resolverCount: number;
  path: string;
  issues: WorkflowAdminIssue[];
}): void {
  const passRule = input.passRule;

  if (passRule.type === "quorum") {
    validateThreshold({
      required: passRule.requiredApprovals,
      eligible: passRule.eligibleApprovals,
      resolverCount: input.resolverCount,
      path: input.path,
      label: "quorum approvals",
      issues: input.issues,
    });
    return;
  }

  if (passRule.type === "percentage") {
    if (passRule.requiredPercentage <= 0 || passRule.requiredPercentage > 100) {
      input.issues.push(
        adminError({
          code: "approval_gate.percentage_invalid",
          path: `${input.path}.requiredPercentage`,
          message: "Required approval percentage must be between 1 and 100.",
          suggestedRepair: "Use a whole percentage in the range 1-100.",
        }),
      );
    }
    validateThreshold({
      required: 1,
      eligible: passRule.eligibleApprovals,
      resolverCount: input.resolverCount,
      path: input.path,
      label: "percentage eligible approvals",
      issues: input.issues,
    });
    return;
  }

  if (passRule.type === "weighted") {
    if (
      passRule.requiredWeight <= 0 ||
      passRule.requiredWeight > passRule.totalWeight
    ) {
      input.issues.push(
        adminError({
          code: "approval_gate.weighted_rule_invalid",
          path: input.path,
          message:
            "Required approval weight must be positive and no greater than total weight.",
          suggestedRepair: "Adjust requiredWeight or totalWeight.",
        }),
      );
    }
    return;
  }

  if (passRule.type === "role_quorum") {
    if (passRule.roleQuorums.length === 0) {
      input.issues.push(
        adminError({
          code: "approval_gate.role_quorum_empty",
          path: `${input.path}.roleQuorums`,
          message: "Role quorum rule needs at least one role quorum.",
          suggestedRepair: "Add one or more role quorum rows.",
        }),
      );
    }

    for (const [roleIndex, roleQuorum] of passRule.roleQuorums.entries()) {
      validateThreshold({
        required: roleQuorum.requiredApprovals,
        eligible: roleQuorum.eligibleApprovals,
        resolverCount: input.resolverCount,
        path: `${input.path}.roleQuorums.${roleIndex}`,
        label: `role quorum ${roleQuorum.role}`,
        issues: input.issues,
      });
    }
    return;
  }

  if (passRule.type === "composite") {
    if (passRule.rules.length === 0) {
      input.issues.push(
        adminError({
          code: "approval_gate.composite_rule_empty",
          path: `${input.path}.rules`,
          message: "Composite approval rule needs child rules.",
          suggestedRepair: "Add child pass rules or use a simpler pass rule.",
        }),
      );
    }

    passRule.rules.forEach((childRule, childIndex) => {
      validatePassRule({
        passRule: childRule,
        resolverCount: input.resolverCount,
        path: `${input.path}.rules.${childIndex}`,
        issues: input.issues,
      });
    });
  }
}

function validateThreshold(input: {
  required: number;
  eligible: number;
  resolverCount: number;
  path: string;
  label: string;
  issues: WorkflowAdminIssue[];
}): void {
  if (input.required <= 0) {
    input.issues.push(
      adminError({
        code: "approval_gate.required_threshold_invalid",
        path: input.path,
        message: `${input.label} must require at least one approval.`,
        suggestedRepair: "Set the required approval threshold above zero.",
      }),
    );
  }

  if (input.required > input.eligible) {
    input.issues.push(
      adminError({
        code: "approval_gate.quorum_impossible",
        path: input.path,
        message: `${input.label} requires more approvals than are eligible.`,
        suggestedRepair: "Increase eligible approvals or lower the required approvals.",
      }),
    );
  }

  if (input.required > input.resolverCount) {
    input.issues.push(
      adminWarning({
        code: "approval_gate.quorum_exceeds_configured_resolvers",
        path: input.path,
        message: `${input.label} may require more approvals than configured resolvers can produce.`,
        suggestedRepair:
          "Add more approver resolvers or confirm dynamic resolvers can produce enough actors.",
      }),
    );
  }
}

function validateFailurePolicies(input: {
  gateConfig: WorkflowApprovalGateConfig;
  graph?: WorkflowGraphConfig;
  path: string;
  issues: WorkflowAdminIssue[];
}): void {
  if (input.gateConfig.failurePolicies.length === 0) {
    input.issues.push(
      adminError({
        code: "approval_gate.failure_policy_missing",
        path: input.path,
        message: "Approval gate needs at least one failure policy.",
        suggestedRepair:
          "Add stop_workflow or send_to_repair as the default failure policy.",
      }),
    );
    return;
  }

  const nodeIds = new Set((input.graph?.nodes ?? []).map((node) => node.nodeId));

  input.gateConfig.failurePolicies.forEach((policy, policyIndex) => {
    const policyPath = `${input.path}.${policyIndex}`;

    if (!supportedFailurePolicyTypes.has(policy.type)) {
      input.issues.push(
        adminError({
          code: "approval_gate.failure_policy_unknown",
          path: `${policyPath}.type`,
          message: `Approval gate failure policy ${policy.type} is not supported.`,
          suggestedRepair: "Use a supported failure policy type.",
        }),
      );
    }

    if (
      policy.nextNodeId !== undefined &&
      input.graph !== undefined &&
      !nodeIds.has(policy.nextNodeId)
    ) {
      input.issues.push(
        adminError({
          code: "approval_gate.failure_policy_next_node_missing",
          path: `${policyPath}.nextNodeId`,
          message: `Failure policy references missing node ${policy.nextNodeId}.`,
          suggestedRepair:
            "Point the failure policy to an existing repair or terminal node.",
        }),
      );
    }

    if (policy.type === "escalate_on_timeout" && blankString(policy.timeoutAfter)) {
      input.issues.push(
        adminError({
          code: "approval_gate.timeout_policy_missing",
          path: `${policyPath}.timeoutAfter`,
          message: "Escalation failure policies need a timeoutAfter value.",
          suggestedRepair: "Set an ISO-8601 duration such as PT48H.",
        }),
      );
    }
  });
}

function validateResolvedApprovers(input: {
  resolvedApprovers: ApprovalGateResolvedApproverPreview[];
  gatePath: string;
  issues: WorkflowAdminIssue[];
}): void {
  for (const approver of input.resolvedApprovers) {
    if (approver.assignmentMode === "unresolved") {
      input.issues.push(
        adminWarning({
          code: "approval_gate.resolver_unresolved_in_preview",
          path: `${input.gatePath}.approverResolvers.${approver.resolverId}`,
          message: `Resolver ${approver.resolverId} did not resolve actors from the provided fixture.`,
          suggestedRepair:
            "Provide richer fixture data or verify the resolver will be populated at runtime.",
        }),
      );
    }
  }
}

function resolveApproverPreview(
  resolver: WorkflowApprovalGateApproverResolverConfig,
  fixture: ApprovalGateResolverFixture | undefined,
): ApprovalGateResolvedApproverPreview {
  const role = resolver.role ?? resolver.type;
  const actors = fixture?.actors ?? [];

  if (resolver.type === "actor" && resolver.actorId !== undefined) {
    return approverPreview({
      resolver,
      actorIds: [resolver.actorId],
      role,
      assignmentMode: "actor",
    });
  }

  if (resolver.type === "role") {
    const actorIds = actors
      .filter(
        (actor) => resolver.role !== undefined && actor.roles.includes(resolver.role),
      )
      .map((actor) => actor.actorId);

    return approverPreview({
      resolver,
      actorIds,
      role,
      assignmentMode: actorIds.length > 0 ? "actor" : "role",
    });
  }

  if (
    resolver.type === "workflow_field" ||
    resolver.type === "relationship" ||
    resolver.type === "org_unit"
  ) {
    const lookupPath =
      resolver.fieldPath ?? resolver.subjectPath ?? resolver.departmentPath;
    if (lookupPath === undefined) {
      return approverPreview({
        resolver,
        actorIds: [],
        role,
        assignmentMode: "unresolved",
      });
    }

    const actorIds = stringValuesAtPath(fixture?.workflowContext ?? {}, lookupPath);

    return approverPreview({
      resolver,
      actorIds,
      role,
      assignmentMode: actorIds.length > 0 ? "actor" : "unresolved",
    });
  }

  return approverPreview({
    resolver,
    actorIds: [],
    role,
    assignmentMode: "unresolved",
  });
}

function approverPreview(input: {
  resolver: WorkflowApprovalGateApproverResolverConfig;
  actorIds: string[];
  role: string;
  assignmentMode: ApprovalGateResolvedApproverPreview["assignmentMode"];
}): ApprovalGateResolvedApproverPreview {
  return {
    resolverId: input.resolver.resolverId,
    actorIds: input.actorIds,
    role: input.role,
    assignmentMode: input.assignmentMode,
    isVetoHolder: input.resolver.isVetoHolder === true,
    weight: input.resolver.weight ?? 1,
  };
}

function blankString(value: unknown): boolean {
  return typeof value !== "string" || value.trim().length === 0;
}

function stringValuesAtPath(source: Record<string, unknown>, path: string): string[] {
  const value = valueAtPath(source, path);

  if (typeof value === "string" && value.trim().length > 0) {
    return [value.trim()];
  }

  if (Array.isArray(value)) {
    return value.flatMap((item) =>
      typeof item === "string" && item.trim().length > 0 ? [item.trim()] : [],
    );
  }

  return [];
}

function valueAtPath(source: Record<string, unknown>, path: string): unknown {
  const pathSegments = path.split(".").filter((segment) => segment.length > 0);
  let currentValue: unknown = source;

  for (const pathSegment of pathSegments) {
    if (typeof currentValue !== "object" || currentValue === null) {
      return undefined;
    }

    currentValue = (currentValue as Record<string, unknown>)[pathSegment];
  }

  return currentValue;
}
