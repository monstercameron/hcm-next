import {
  err,
  ok,
  permissionDeniedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type {
  AccessGrantRecord,
  ActorRecord,
  EmployeeProjectionDocument,
  EmployeeProjectionRecord,
  OrganizationRelationshipRecord,
  OrganizationUnitRecord,
  RoleBindingRecord,
  WorkerAssignmentRecord,
} from "@human-capital-management-suite/data-store";

export type EmployeeFieldGroup =
  | "profile"
  | "organization"
  | "job"
  | "employment"
  | "compensation"
  | "contact"
  | "emergencyContacts"
  | "workflow";

export type PopulationScopeType =
  | "self"
  | "direct_reports"
  | "manager_chain"
  | "business_unit"
  | "department"
  | "team"
  | "org_unit"
  | "org_unit_descendants"
  | "legal_entity"
  | "location"
  | "country"
  | "cost_center"
  | "project"
  | "assignment"
  | "workflow_instance"
  | "global";

type RelationshipScopeType = "self" | "direct_reports" | "manager_chain" | "global";

type ValuedScopeType = Exclude<PopulationScopeType, RelationshipScopeType>;

export type EmployeeAccessScope =
  | {
      type: RelationshipScopeType;
    }
  | {
      type: ValuedScopeType;
      values: readonly string[];
    };

export type EmployeeAccessGrant = {
  grantId?: string;
  actorIds?: readonly string[];
  roles?: readonly string[];
  fieldGroups: readonly EmployeeFieldGroup[] | "all";
  scopes: readonly EmployeeAccessScope[];
};

export type EmployeeAccessGrants = readonly (EmployeeAccessGrant | AccessGrantRecord)[];

export type EmployeeAccessEvaluationContext = {
  legacyGrants?: EmployeeAccessGrants;
  roleBindings?: readonly RoleBindingRecord[];
  workerAssignments?: readonly WorkerAssignmentRecord[];
  proposedWorkerAssignments?: readonly WorkerAssignmentRecord[];
  organizationRelationships?: readonly OrganizationRelationshipRecord[];
  organizationUnits?: readonly OrganizationUnitRecord[];
  workflowInstanceId?: string;
  evaluatedAt?: string;
  includeProposedAssignments?: boolean;
};

export type EmployeeAccessInput =
  | EmployeeAccessGrants
  | EmployeeAccessEvaluationContext;

export type EmployeeAccessDecisionKind = "allow" | "deny" | "mask" | "redact";

export type EmployeeAccessDecision = {
  decision: EmployeeAccessDecisionKind;
  actorId: string;
  resourceType: "employee";
  resourceId: string;
  action: string;
  fieldGroups: readonly EmployeeFieldGroup[];
  evaluatedAt: string;
  reason: string;
  matchedWorkerAssignmentIds: readonly string[];
  matchedOrgUnitIds: readonly string[];
  roleBindingId?: string;
  grantId?: string;
  policyId?: string;
  scopeType?: PopulationScopeType;
  scopeId?: string;
  relationshipType?: string;
  workflowInstanceId?: string;
};

export const SUPPORTED_EMPLOYEE_FIELD_GROUPS: readonly EmployeeFieldGroup[] = [
  "profile",
  "organization",
  "job",
  "employment",
  "compensation",
  "contact",
  "emergencyContacts",
  "workflow",
] as const;

export const MASKED_EMPLOYEE_FIELD_VALUE = "[restricted]";

const accessDecisionKindsThatDenyRead = new Set<EmployeeAccessDecisionKind>([
  "deny",
  "mask",
  "redact",
]);

/**
 * Returns an auditable employee access decision for one actor, resource, and field group.
 */
export function evaluateEmployeeAccess(input: {
  actor: ActorRecord;
  employeeDocument: EmployeeProjectionDocument;
  indexedFields?: Record<string, unknown>;
  fieldGroup: EmployeeFieldGroup;
  access: EmployeeAccessInput;
}): EmployeeAccessDecision {
  const accessContext = normalizeAccessContext(input.access);
  const evaluatedAt = accessContext.evaluatedAt ?? new Date().toISOString();
  const indexedFields = input.indexedFields ?? {};
  const baseDecision = {
    actorId: input.actor.actorId,
    resourceType: "employee" as const,
    resourceId: input.employeeDocument.employeeId,
    action: actionForFieldGroup(input.fieldGroup),
    fieldGroups: [input.fieldGroup],
    evaluatedAt,
    matchedWorkerAssignmentIds: [],
    matchedOrgUnitIds: [],
    ...(accessContext.workflowInstanceId !== undefined
      ? { workflowInstanceId: accessContext.workflowInstanceId }
      : {}),
  };

  if (input.actor.status !== "active") {
    return {
      ...baseDecision,
      decision: "deny",
      reason: "actor_inactive",
    };
  }

  const explicitDenyDecision = findRoleBindingDecision({
    actor: input.actor,
    employeeDocument: input.employeeDocument,
    indexedFields,
    accessContext,
    fieldGroup: input.fieldGroup,
    evaluatedAt,
    effects: accessDecisionKindsThatDenyRead,
  });

  if (explicitDenyDecision !== undefined) {
    return explicitDenyDecision;
  }

  const legacyGrantDecision = findLegacyGrantDecision({
    actor: input.actor,
    employeeDocument: input.employeeDocument,
    indexedFields,
    accessContext,
    fieldGroup: input.fieldGroup,
    evaluatedAt,
  });

  if (legacyGrantDecision !== undefined) {
    return legacyGrantDecision;
  }

  const roleBindingDecision = findRoleBindingDecision({
    actor: input.actor,
    employeeDocument: input.employeeDocument,
    indexedFields,
    accessContext,
    fieldGroup: input.fieldGroup,
    evaluatedAt,
    effects: new Set<EmployeeAccessDecisionKind>(["allow"]),
  });

  if (roleBindingDecision !== undefined) {
    return roleBindingDecision;
  }

  return {
    ...baseDecision,
    decision: "deny",
    reason: "no_matching_scope",
  };
}

/**
 * Checks whether an actor may view a field group on one employee document.
 */
export function canViewEmployee(
  actor: ActorRecord,
  employeeDocument: EmployeeProjectionDocument,
  fieldGroup: EmployeeFieldGroup,
  access: EmployeeAccessInput,
): Result<true, AppError> {
  const decision = evaluateEmployeeAccess({
    actor,
    employeeDocument,
    fieldGroup,
    access,
  });

  if (decision.decision !== "allow") {
    return err(
      permissionDeniedError({
        actorId: actor.actorId,
        action: decision.action,
        resourceType: decision.resourceType,
        resourceId: decision.resourceId,
        employeeId: employeeDocument.employeeId,
        fieldGroup,
        missingScope: decision.scopeType ?? "employee_visibility",
        decision,
      }),
    );
  }

  return ok(true);
}

/**
 * Returns a projection with unauthorized field groups masked.
 */
export function filterEmployeeProjectionForActor(
  actor: ActorRecord,
  projectionRecord: EmployeeProjectionRecord,
  access: EmployeeAccessInput,
): Result<EmployeeProjectionRecord, AppError> {
  if (!visibleEmployeeProjectionForActor(actor, projectionRecord, access)) {
    return err(
      permissionDeniedError({
        actorId: actor.actorId,
        action: "employee.view",
        resourceType: "employee",
        resourceId: projectionRecord.employeeId,
        employeeId: projectionRecord.employeeId,
        fieldGroup: "employee",
        missingScope: "employee_visibility",
      }),
    );
  }

  const filteredProjection = cloneProjection(projectionRecord);
  const documentRecord = filteredProjection.document as EmployeeDocumentRecord;

  for (const fieldGroup of SUPPORTED_EMPLOYEE_FIELD_GROUPS) {
    const decision = evaluateEmployeeAccess({
      actor,
      employeeDocument: projectionRecord.document,
      indexedFields: projectionRecord.indexedFields,
      fieldGroup,
      access,
    });

    if (decision.decision !== "allow") {
      maskEmployeeFieldGroup(documentRecord, fieldGroup);
      maskEmployeeIndexedFields(filteredProjection.indexedFields, fieldGroup);
    }
  }

  return ok(filteredProjection);
}

/**
 * List-view predicate: an employee is visible when any supported field group is allowed.
 */
export function visibleEmployeeProjectionForActor(
  actor: ActorRecord,
  projectionRecord: EmployeeProjectionRecord,
  access: EmployeeAccessInput,
): boolean {
  return SUPPORTED_EMPLOYEE_FIELD_GROUPS.some((fieldGroup) => {
    const decision = evaluateEmployeeAccess({
      actor,
      employeeDocument: projectionRecord.document,
      indexedFields: projectionRecord.indexedFields,
      fieldGroup,
      access,
    });

    return decision.decision === "allow";
  });
}

/**
 * Masks a workflow summary using employee field-group visibility.
 */
export function filterWorkflowSummaryForActor(input: {
  actor: ActorRecord;
  projectionRecord: EmployeeProjectionRecord;
  workflowSummary: Record<string, unknown>;
  access: EmployeeAccessInput;
}): Result<Record<string, unknown>, AppError> {
  const workflowDecision = evaluateEmployeeAccess({
    actor: input.actor,
    employeeDocument: input.projectionRecord.document,
    indexedFields: input.projectionRecord.indexedFields,
    fieldGroup: "workflow",
    access: input.access,
  });

  if (workflowDecision.decision !== "allow") {
    return err(
      permissionDeniedError({
        actorId: input.actor.actorId,
        action: workflowDecision.action,
        resourceType: workflowDecision.resourceType,
        resourceId: workflowDecision.resourceId,
        employeeId: input.projectionRecord.employeeId,
        fieldGroup: "workflow",
        missingScope: workflowDecision.scopeType ?? "workflow_instance",
        decision: workflowDecision,
      }),
    );
  }

  const allowedFieldGroups = new Set<EmployeeFieldGroup>(["workflow"]);

  for (const fieldGroup of SUPPORTED_EMPLOYEE_FIELD_GROUPS) {
    const decision = evaluateEmployeeAccess({
      actor: input.actor,
      employeeDocument: input.projectionRecord.document,
      indexedFields: input.projectionRecord.indexedFields,
      fieldGroup,
      access: input.access,
    });

    if (decision.decision === "allow") {
      allowedFieldGroups.add(fieldGroup);
    }
  }

  return ok(
    maskWorkflowSummaryValue(input.workflowSummary, allowedFieldGroups) as Record<
      string,
      unknown
    >,
  );
}

type EmployeeDocumentRecord = EmployeeProjectionDocument & Record<string, unknown>;

type NormalizedEmployeeAccessContext = Required<
  Pick<
    EmployeeAccessEvaluationContext,
    | "legacyGrants"
    | "roleBindings"
    | "workerAssignments"
    | "proposedWorkerAssignments"
    | "organizationRelationships"
    | "organizationUnits"
  >
> &
  Pick<
    EmployeeAccessEvaluationContext,
    "workflowInstanceId" | "evaluatedAt" | "includeProposedAssignments"
  >;

type ScopeMatchResult = {
  matched: boolean;
  matchedWorkerAssignmentIds: string[];
  matchedOrgUnitIds: string[];
  scopeId?: string;
};

function normalizeAccessContext(
  access: EmployeeAccessInput,
): NormalizedEmployeeAccessContext {
  if (isEmployeeAccessGrants(access)) {
    return {
      legacyGrants: access,
      roleBindings: [],
      workerAssignments: [],
      proposedWorkerAssignments: [],
      organizationRelationships: [],
      organizationUnits: [],
    };
  }

  return {
    legacyGrants: access.legacyGrants ?? [],
    roleBindings: access.roleBindings ?? [],
    workerAssignments: access.workerAssignments ?? [],
    proposedWorkerAssignments: access.proposedWorkerAssignments ?? [],
    organizationRelationships: access.organizationRelationships ?? [],
    organizationUnits: access.organizationUnits ?? [],
    ...(access.workflowInstanceId !== undefined
      ? { workflowInstanceId: access.workflowInstanceId }
      : {}),
    ...(access.evaluatedAt !== undefined ? { evaluatedAt: access.evaluatedAt } : {}),
    ...(access.includeProposedAssignments !== undefined
      ? { includeProposedAssignments: access.includeProposedAssignments }
      : {}),
  };
}

function isEmployeeAccessGrants(
  access: EmployeeAccessInput,
): access is EmployeeAccessGrants {
  return Array.isArray(access);
}

function findLegacyGrantDecision(input: {
  actor: ActorRecord;
  employeeDocument: EmployeeProjectionDocument;
  indexedFields: Record<string, unknown>;
  accessContext: NormalizedEmployeeAccessContext;
  fieldGroup: EmployeeFieldGroup;
  evaluatedAt: string;
}): EmployeeAccessDecision | undefined {
  for (const grant of input.accessContext.legacyGrants) {
    if (!grantAllowsFieldGroup(grant, input.fieldGroup)) {
      continue;
    }

    if (!actorMatchesGrant(input.actor, grant)) {
      continue;
    }

    const matchingScope = grantScopes(grant).find((scope) => {
      return scopeMatchesEmployee({
        actor: input.actor,
        employeeDocument: input.employeeDocument,
        indexedFields: input.indexedFields,
        scope,
      });
    });

    if (matchingScope === undefined) {
      continue;
    }

    const grantId = grantIdForGrant(grant);
    const scopeValues = scopeValuesForDecision(matchingScope);

    return {
      actorId: input.actor.actorId,
      resourceType: "employee",
      resourceId: input.employeeDocument.employeeId,
      action: actionForFieldGroup(input.fieldGroup),
      fieldGroups: [input.fieldGroup],
      evaluatedAt: input.evaluatedAt,
      decision: "allow",
      reason: "legacy_access_grant_matched",
      matchedWorkerAssignmentIds: [],
      matchedOrgUnitIds: [],
      ...(grantId !== undefined ? { grantId } : {}),
      scopeType: matchingScope.type,
      ...(scopeValues[0] !== undefined ? { scopeId: scopeValues[0] } : {}),
      ...(input.accessContext.workflowInstanceId !== undefined
        ? { workflowInstanceId: input.accessContext.workflowInstanceId }
        : {}),
    };
  }

  return undefined;
}

function findRoleBindingDecision(input: {
  actor: ActorRecord;
  employeeDocument: EmployeeProjectionDocument;
  indexedFields: Record<string, unknown>;
  accessContext: NormalizedEmployeeAccessContext;
  fieldGroup: EmployeeFieldGroup;
  evaluatedAt: string;
  effects: ReadonlySet<EmployeeAccessDecisionKind>;
}): EmployeeAccessDecision | undefined {
  for (const roleBinding of input.accessContext.roleBindings) {
    const effect = effectForRoleBinding(roleBinding);

    if (!input.effects.has(effect)) {
      continue;
    }

    if (
      !roleBindingIsEffectiveForActor(roleBinding, input.actor, input.evaluatedAt) ||
      !roleBindingAllowsFieldGroup(roleBinding, input.fieldGroup)
    ) {
      continue;
    }

    const scopeMatch = roleBindingScopeMatchesEmployee({
      actor: input.actor,
      employeeDocument: input.employeeDocument,
      indexedFields: input.indexedFields,
      roleBinding,
      accessContext: input.accessContext,
      evaluatedAt: input.evaluatedAt,
    });

    if (!scopeMatch.matched) {
      continue;
    }

    return {
      actorId: input.actor.actorId,
      resourceType: "employee",
      resourceId: input.employeeDocument.employeeId,
      action: actionForFieldGroup(input.fieldGroup),
      fieldGroups: [input.fieldGroup],
      evaluatedAt: input.evaluatedAt,
      decision: effect,
      reason:
        effect === "allow"
          ? "role_binding_scope_matched"
          : "role_binding_explicit_restriction_matched",
      matchedWorkerAssignmentIds: scopeMatch.matchedWorkerAssignmentIds,
      matchedOrgUnitIds: scopeMatch.matchedOrgUnitIds,
      roleBindingId: roleBinding.roleBindingId,
      policyId: roleBinding.roleKey,
      scopeType: roleBinding.scopeType,
      ...(scopeMatch.scopeId !== undefined ? { scopeId: scopeMatch.scopeId } : {}),
      ...(roleBinding.relationshipType !== undefined
        ? { relationshipType: roleBinding.relationshipType }
        : {}),
      ...(input.accessContext.workflowInstanceId !== undefined
        ? { workflowInstanceId: input.accessContext.workflowInstanceId }
        : {}),
    };
  }

  return undefined;
}

function grantAllowsFieldGroup(
  grant: EmployeeAccessGrant | AccessGrantRecord,
  fieldGroup: EmployeeFieldGroup,
): boolean {
  const fieldGroups = grant.fieldGroups;

  if (fieldGroups === "all") {
    return true;
  }

  return fieldGroups.some((grantedFieldGroup) => {
    return normalizeFieldGroup(grantedFieldGroup) === fieldGroup;
  });
}

function actorMatchesGrant(
  actor: ActorRecord,
  grant: EmployeeAccessGrant | AccessGrantRecord,
): boolean {
  if (actor.status !== "active") {
    return false;
  }

  if ("actorId" in grant) {
    return (
      grant.actorId === actor.actorId && grantIsActive(grant, new Date().toISOString())
    );
  }

  const actorIdMatches =
    grant.actorIds === undefined || grant.actorIds.includes(actor.actorId);
  const roleMatches =
    grant.roles === undefined ||
    actor.roles.some((role) => grant.roles?.includes(role));

  return actorIdMatches && roleMatches;
}

function grantIsActive(grant: AccessGrantRecord, evaluatedAt: string): boolean {
  return (
    grant.status === "active" &&
    grant.startsAt <= evaluatedAt &&
    (grant.expiresAt === undefined || grant.expiresAt > evaluatedAt)
  );
}

function grantScopes(
  grant: EmployeeAccessGrant | AccessGrantRecord,
): EmployeeAccessScope[] {
  if ("scope" in grant) {
    return [accessGrantScopeToEmployeeScope(grant.scope)];
  }

  return [...grant.scopes];
}

function accessGrantScopeToEmployeeScope(scope: AccessGrantRecord["scope"]) {
  if (isValuedScopeType(scope.type)) {
    return {
      type: scope.type,
      values: scope.values ?? [],
    };
  }

  return {
    type: scope.type,
  };
}

function roleBindingIsEffectiveForActor(
  roleBinding: RoleBindingRecord,
  actor: ActorRecord,
  evaluatedAt: string,
): boolean {
  return (
    roleBinding.tenantId === actor.tenantId &&
    roleBinding.actorId === actor.actorId &&
    roleBinding.status === "active" &&
    roleBinding.effectiveStart <= evaluatedAt &&
    (roleBinding.effectiveEnd === undefined || roleBinding.effectiveEnd > evaluatedAt)
  );
}

function roleBindingAllowsFieldGroup(
  roleBinding: RoleBindingRecord,
  fieldGroup: EmployeeFieldGroup,
): boolean {
  const metadataFieldGroups = fieldGroupsFromMetadata(
    roleBinding.metadata,
    "fieldGroups",
  );

  if (fieldGroupSetIncludes(metadataFieldGroups, fieldGroup)) {
    return true;
  }

  const restrictedFieldGroups = [
    fieldGroupsFromMetadata(roleBinding.metadata, "denyFieldGroups"),
    fieldGroupsFromMetadata(roleBinding.metadata, "deniedFieldGroups"),
    fieldGroupsFromMetadata(roleBinding.metadata, "maskFieldGroups"),
    fieldGroupsFromMetadata(roleBinding.metadata, "redactFieldGroups"),
  ];

  return restrictedFieldGroups.some((fieldGroups) => {
    return fieldGroupSetIncludes(fieldGroups, fieldGroup);
  });
}

function effectForRoleBinding(
  roleBinding: RoleBindingRecord,
): EmployeeAccessDecisionKind {
  const effect = stringFromMetadata(roleBinding.metadata, "effect");

  if (effect === "deny" || effect === "mask" || effect === "redact") {
    return effect;
  }

  return "allow";
}

function fieldGroupsFromMetadata(
  metadata: Record<string, unknown>,
  key: string,
): EmployeeFieldGroup[] | "all" {
  const value = metadata[key];

  if (value === "all") {
    return "all";
  }

  if (!Array.isArray(value)) {
    return [];
  }

  if (value.includes("all")) {
    return "all";
  }

  return value.flatMap((fieldGroup) => {
    if (typeof fieldGroup !== "string") {
      return [];
    }

    const normalizedFieldGroup = normalizeFieldGroup(fieldGroup);

    return normalizedFieldGroup === undefined ? [] : [normalizedFieldGroup];
  });
}

function fieldGroupSetIncludes(
  fieldGroups: EmployeeFieldGroup[] | "all",
  fieldGroup: EmployeeFieldGroup,
): boolean {
  return fieldGroups === "all" || fieldGroups.includes(fieldGroup);
}

function roleBindingScopeMatchesEmployee(input: {
  actor: ActorRecord;
  employeeDocument: EmployeeProjectionDocument;
  indexedFields: Record<string, unknown>;
  roleBinding: RoleBindingRecord;
  accessContext: NormalizedEmployeeAccessContext;
  evaluatedAt: string;
}): ScopeMatchResult {
  const assignments = assignmentsForEmployee({
    employeeId: input.employeeDocument.employeeId,
    accessContext: input.accessContext,
    evaluatedAt: input.evaluatedAt,
  });

  if (input.roleBinding.scopeType === "global") {
    return {
      matched: true,
      matchedWorkerAssignmentIds: assignments.active.map((assignment) => {
        return assignment.workerAssignmentId;
      }),
      matchedOrgUnitIds: assignments.active.map((assignment) => assignment.orgUnitId),
    };
  }

  if (input.roleBinding.scopeType === "self") {
    return relationshipScopeMatch(
      input.actor.linkedWorkerId === input.employeeDocument.employeeId,
      [],
      [],
    );
  }

  if (input.roleBinding.scopeType === "direct_reports") {
    const matchingAssignments = assignments.active.filter((assignment) => {
      return (
        input.actor.linkedWorkerId !== undefined &&
        assignment.managerEmployeeId === input.actor.linkedWorkerId
      );
    });

    if (matchingAssignments.length > 0) {
      return assignmentsScopeMatch(matchingAssignments);
    }

    return relationshipScopeMatch(
      input.actor.linkedWorkerId !== undefined &&
        input.actor.linkedWorkerId === input.employeeDocument.manager.employeeId,
      [],
      [],
    );
  }

  if (input.roleBinding.scopeType === "manager_chain") {
    return relationshipScopeMatch(
      input.actor.linkedWorkerId !== undefined &&
        valueListForPaths(input.employeeDocument, input.indexedFields, [
          "manager.chain",
          "manager.managerChain",
          "manager.managerChainEmployeeIds",
          "organization.managerChain",
          "organization.managerChainEmployeeIds",
          "custom.managerChain",
          "custom.managerChainEmployeeIds",
        ]).includes(input.actor.linkedWorkerId),
      [],
      [],
    );
  }

  if (input.roleBinding.scopeType === "workflow_instance") {
    return workflowInstanceScopeMatches(input.roleBinding, input.accessContext);
  }

  if (input.roleBinding.scopeType === "org_unit") {
    return roleBindingOrgUnitScopeMatches(
      input.roleBinding,
      assignments.allVisible,
      input.accessContext,
    );
  }

  if (input.roleBinding.scopeType === "org_unit_descendants") {
    return roleBindingOrgUnitDescendantsScopeMatches({
      roleBinding: input.roleBinding,
      assignments: assignments.allVisible,
      accessContext: input.accessContext,
      evaluatedAt: input.evaluatedAt,
    });
  }

  if (input.roleBinding.scopeType === "assignment") {
    return roleBindingAssignmentScopeMatches(input.roleBinding, assignments.allVisible);
  }

  if (input.roleBinding.scopeType === "project") {
    return roleBindingTypedAssignmentScopeMatches({
      roleBinding: input.roleBinding,
      assignments: assignments.allVisible,
      assignmentTypes: ["project"],
      projectionValues: employeeValuesForScope(
        "project",
        input.employeeDocument,
        input.indexedFields,
      ),
      accessContext: input.accessContext,
    });
  }

  if (input.roleBinding.scopeType === "cost_center") {
    return roleBindingTypedAssignmentScopeMatches({
      roleBinding: input.roleBinding,
      assignments: assignments.allVisible,
      assignmentTypes: ["cost_center"],
      projectionValues: employeeValuesForScope(
        "cost_center",
        input.employeeDocument,
        input.indexedFields,
      ),
      accessContext: input.accessContext,
    });
  }

  if (input.roleBinding.scopeType === "location") {
    return roleBindingTypedAssignmentScopeMatches({
      roleBinding: input.roleBinding,
      assignments: assignments.allVisible,
      assignmentTypes: ["work_location"],
      projectionValues: employeeValuesForScope(
        "location",
        input.employeeDocument,
        input.indexedFields,
      ),
      accessContext: input.accessContext,
    });
  }

  if (input.roleBinding.scopeType === "legal_entity") {
    return roleBindingTypedAssignmentScopeMatches({
      roleBinding: input.roleBinding,
      assignments: assignments.allVisible,
      assignmentTypes: ["legal_employer"],
      projectionValues: employeeValuesForScope(
        "legal_entity",
        input.employeeDocument,
        input.indexedFields,
      ),
      accessContext: input.accessContext,
    });
  }

  if (input.roleBinding.scopeType === "country") {
    return roleBindingCountryScopeMatches(input.roleBinding, assignments.allVisible, {
      employeeDocument: input.employeeDocument,
      indexedFields: input.indexedFields,
      accessContext: input.accessContext,
    });
  }

  return relationshipScopeMatch(false, [], []);
}

function assignmentsForEmployee(input: {
  employeeId: string;
  accessContext: NormalizedEmployeeAccessContext;
  evaluatedAt: string;
}): {
  active: WorkerAssignmentRecord[];
  allVisible: WorkerAssignmentRecord[];
} {
  const active = input.accessContext.workerAssignments.filter((assignment) => {
    return (
      assignment.employeeId === input.employeeId &&
      assignmentIsActiveAt(assignment, input.evaluatedAt)
    );
  });
  const proposed = input.accessContext.includeProposedAssignments
    ? input.accessContext.proposedWorkerAssignments.filter((assignment) => {
        return assignment.employeeId === input.employeeId;
      })
    : [];

  return {
    active,
    allVisible: [...active, ...proposed],
  };
}

function assignmentIsActiveAt(
  assignment: WorkerAssignmentRecord,
  evaluatedAt: string,
): boolean {
  return (
    assignment.status === "active" &&
    assignment.effectiveStart <= evaluatedAt &&
    (assignment.effectiveEnd === undefined || assignment.effectiveEnd > evaluatedAt)
  );
}

function relationshipScopeMatch(
  matched: boolean,
  matchedWorkerAssignmentIds: string[],
  matchedOrgUnitIds: string[],
): ScopeMatchResult {
  return {
    matched,
    matchedWorkerAssignmentIds,
    matchedOrgUnitIds,
  };
}

function assignmentsScopeMatch(assignments: readonly WorkerAssignmentRecord[]) {
  return {
    matched: assignments.length > 0,
    matchedWorkerAssignmentIds: assignments.map((assignment) => {
      return assignment.workerAssignmentId;
    }),
    matchedOrgUnitIds: assignments.map((assignment) => {
      return assignment.orgUnitId;
    }),
    ...(assignments[0] !== undefined ? { scopeId: assignments[0].orgUnitId } : {}),
  };
}

function workflowInstanceScopeMatches(
  roleBinding: RoleBindingRecord,
  accessContext: NormalizedEmployeeAccessContext,
): ScopeMatchResult {
  const workflowInstanceIds = [
    roleBinding.scopeValue,
    roleBinding.sourceWorkflowInstanceId,
  ].filter((value): value is string => value !== undefined);

  return {
    matched:
      accessContext.workflowInstanceId !== undefined &&
      workflowInstanceIds.includes(accessContext.workflowInstanceId),
    matchedWorkerAssignmentIds: [],
    matchedOrgUnitIds: [],
    ...(accessContext.workflowInstanceId !== undefined
      ? { scopeId: accessContext.workflowInstanceId }
      : {}),
  };
}

function roleBindingOrgUnitScopeMatches(
  roleBinding: RoleBindingRecord,
  assignments: readonly WorkerAssignmentRecord[],
  accessContext: NormalizedEmployeeAccessContext,
): ScopeMatchResult {
  const scopeOrgUnitIds = roleBindingScopeOrgUnitIds(roleBinding, accessContext);
  const matchingAssignments = assignments.filter((assignment) => {
    return scopeOrgUnitIds.includes(assignment.orgUnitId);
  });

  return assignmentsScopeMatch(matchingAssignments);
}

function roleBindingOrgUnitDescendantsScopeMatches(input: {
  roleBinding: RoleBindingRecord;
  assignments: readonly WorkerAssignmentRecord[];
  accessContext: NormalizedEmployeeAccessContext;
  evaluatedAt: string;
}): ScopeMatchResult {
  const scopeOrgUnitIds = roleBindingScopeOrgUnitIds(
    input.roleBinding,
    input.accessContext,
  );
  const descendantOrgUnitIds = new Set<string>();

  for (const scopeOrgUnitId of scopeOrgUnitIds) {
    for (const descendantOrgUnitId of resolveOrgUnitDescendantIds({
      scopeOrgUnitId,
      relationships: input.accessContext.organizationRelationships,
      evaluatedAt: input.evaluatedAt,
    })) {
      descendantOrgUnitIds.add(descendantOrgUnitId);
    }
  }

  const matchingAssignments = input.assignments.filter((assignment) => {
    return descendantOrgUnitIds.has(assignment.orgUnitId);
  });

  return {
    ...assignmentsScopeMatch(matchingAssignments),
    ...(scopeOrgUnitIds[0] !== undefined ? { scopeId: scopeOrgUnitIds[0] } : {}),
  };
}

function roleBindingAssignmentScopeMatches(
  roleBinding: RoleBindingRecord,
  assignments: readonly WorkerAssignmentRecord[],
): ScopeMatchResult {
  const scopeValues = stringValues(roleBinding.scopeValue);
  const matchingAssignments = assignments.filter((assignment) => {
    return (
      scopeValues.includes(assignment.workerAssignmentId) ||
      scopeValues.includes(assignment.assignmentType) ||
      (roleBinding.scopeOrgUnitId !== undefined &&
        assignment.orgUnitId === roleBinding.scopeOrgUnitId)
    );
  });

  return assignmentsScopeMatch(matchingAssignments);
}

function roleBindingTypedAssignmentScopeMatches(input: {
  roleBinding: RoleBindingRecord;
  assignments: readonly WorkerAssignmentRecord[];
  assignmentTypes: readonly string[];
  projectionValues: readonly string[];
  accessContext: NormalizedEmployeeAccessContext;
}): ScopeMatchResult {
  const roleBindingOrgUnitIds = roleBindingScopeOrgUnitIds(
    input.roleBinding,
    input.accessContext,
  );
  const scopeValues = roleBindingScopeValues(input.roleBinding, input.accessContext);
  const matchingAssignments = input.assignments.filter((assignment) => {
    return (
      input.assignmentTypes.includes(assignment.assignmentType) &&
      (roleBindingOrgUnitIds.includes(assignment.orgUnitId) ||
        orgUnitMatchesScopeValues(
          assignment.orgUnitId,
          scopeValues,
          input.accessContext,
        ) ||
        assignmentMetadataMatchesScopeValues(assignment, scopeValues))
    );
  });

  if (matchingAssignments.length > 0) {
    return assignmentsScopeMatch(matchingAssignments);
  }

  return {
    matched: input.projectionValues.some((value) => scopeValues.includes(value)),
    matchedWorkerAssignmentIds: [],
    matchedOrgUnitIds: [],
    ...(scopeValues[0] !== undefined ? { scopeId: scopeValues[0] } : {}),
  };
}

function roleBindingCountryScopeMatches(
  roleBinding: RoleBindingRecord,
  assignments: readonly WorkerAssignmentRecord[],
  input: {
    employeeDocument: EmployeeProjectionDocument;
    indexedFields: Record<string, unknown>;
    accessContext: NormalizedEmployeeAccessContext;
  },
): ScopeMatchResult {
  const scopeValues = roleBindingScopeValues(roleBinding, input.accessContext);
  const matchingAssignments = assignments.filter((assignment) => {
    const orgUnit = organizationUnitById(input.accessContext, assignment.orgUnitId);

    return orgUnit?.country !== undefined && scopeValues.includes(orgUnit.country);
  });

  if (matchingAssignments.length > 0) {
    return assignmentsScopeMatch(matchingAssignments);
  }

  const countryValues = employeeValuesForScope(
    "country",
    input.employeeDocument,
    input.indexedFields,
  );

  return {
    matched: countryValues.some((value) => scopeValues.includes(value)),
    matchedWorkerAssignmentIds: [],
    matchedOrgUnitIds: [],
    ...(scopeValues[0] !== undefined ? { scopeId: scopeValues[0] } : {}),
  };
}

function roleBindingScopeOrgUnitIds(
  roleBinding: RoleBindingRecord,
  accessContext: NormalizedEmployeeAccessContext,
): string[] {
  const explicitOrgUnitIds = [
    roleBinding.scopeOrgUnitId,
    orgUnitIdFromScopeValue(roleBinding.scopeValue, accessContext),
  ].filter((value): value is string => value !== undefined);

  return [...new Set(explicitOrgUnitIds)];
}

function roleBindingScopeValues(
  roleBinding: RoleBindingRecord,
  accessContext: NormalizedEmployeeAccessContext,
): string[] {
  const values = stringValues(roleBinding.scopeValue);

  if (roleBinding.scopeOrgUnitId === undefined) {
    return values;
  }

  const orgUnit = organizationUnitById(accessContext, roleBinding.scopeOrgUnitId);

  return [
    ...values,
    roleBinding.scopeOrgUnitId,
    ...(orgUnit === undefined
      ? []
      : [orgUnit.orgUnitId, orgUnit.unitKey, orgUnit.name, orgUnit.country].filter(
          (value): value is string => value !== undefined,
        )),
  ];
}

function orgUnitIdFromScopeValue(
  scopeValue: string | undefined,
  accessContext: NormalizedEmployeeAccessContext,
): string | undefined {
  if (scopeValue === undefined) {
    return undefined;
  }

  const matchingOrgUnit = accessContext.organizationUnits.find((orgUnit) => {
    return (
      orgUnit.orgUnitId === scopeValue ||
      orgUnit.unitKey === scopeValue ||
      orgUnit.name === scopeValue
    );
  });

  return matchingOrgUnit?.orgUnitId;
}

function organizationUnitById(
  accessContext: NormalizedEmployeeAccessContext,
  orgUnitId: string,
): OrganizationUnitRecord | undefined {
  return accessContext.organizationUnits.find((orgUnit) => {
    return orgUnit.orgUnitId === orgUnitId;
  });
}

function orgUnitMatchesScopeValues(
  orgUnitId: string,
  scopeValues: readonly string[],
  accessContext: NormalizedEmployeeAccessContext,
): boolean {
  const orgUnit = organizationUnitById(accessContext, orgUnitId);

  if (orgUnit === undefined) {
    return scopeValues.includes(orgUnitId);
  }

  return [orgUnit.orgUnitId, orgUnit.unitKey, orgUnit.name, orgUnit.country].some(
    (value) => value !== undefined && scopeValues.includes(value),
  );
}

function assignmentMetadataMatchesScopeValues(
  assignment: WorkerAssignmentRecord,
  scopeValues: readonly string[],
): boolean {
  return Object.values(assignment.metadata).some((value) => {
    return stringValues(value).some((metadataValue) => {
      return scopeValues.includes(metadataValue);
    });
  });
}

function resolveOrgUnitDescendantIds(input: {
  scopeOrgUnitId: string;
  relationships: readonly OrganizationRelationshipRecord[];
  evaluatedAt: string;
}): string[] {
  const descendants = new Set<string>([input.scopeOrgUnitId]);
  const queue = [input.scopeOrgUnitId];

  while (queue.length > 0) {
    const parentOrgUnitId = queue.shift();

    if (parentOrgUnitId === undefined) {
      continue;
    }

    const childRelationships = input.relationships.filter((relationship) => {
      return (
        relationship.toOrgUnitId === parentOrgUnitId &&
        relationship.status === "active" &&
        relationship.effectiveStart <= input.evaluatedAt &&
        (relationship.effectiveEnd === undefined ||
          relationship.effectiveEnd > input.evaluatedAt)
      );
    });

    for (const childRelationship of childRelationships) {
      if (descendants.has(childRelationship.fromOrgUnitId)) {
        continue;
      }

      descendants.add(childRelationship.fromOrgUnitId);
      queue.push(childRelationship.fromOrgUnitId);
    }
  }

  return [...descendants];
}

function scopeMatchesEmployee(input: {
  actor: ActorRecord;
  employeeDocument: EmployeeProjectionDocument;
  indexedFields: Record<string, unknown>;
  scope: EmployeeAccessScope;
}): boolean {
  if (input.scope.type === "global") {
    return true;
  }

  if (input.scope.type === "self") {
    return (
      input.actor.linkedWorkerId !== undefined &&
      input.actor.linkedWorkerId === input.employeeDocument.employeeId
    );
  }

  if (input.scope.type === "direct_reports") {
    return (
      input.actor.linkedWorkerId !== undefined &&
      input.actor.linkedWorkerId === input.employeeDocument.manager.employeeId
    );
  }

  if (input.scope.type === "manager_chain") {
    return (
      input.actor.linkedWorkerId !== undefined &&
      valueListForPaths(input.employeeDocument, input.indexedFields, [
        "manager.chain",
        "manager.managerChain",
        "manager.managerChainEmployeeIds",
        "organization.managerChain",
        "organization.managerChainEmployeeIds",
        "custom.managerChain",
        "custom.managerChainEmployeeIds",
      ]).includes(input.actor.linkedWorkerId)
    );
  }

  if (!isDimensionScope(input.scope)) {
    return false;
  }

  const employeeValues = employeeValuesForScope(
    input.scope.type,
    input.employeeDocument,
    input.indexedFields,
  );

  return input.scope.values.some((value) => employeeValues.includes(value));
}

function isDimensionScope(
  scope: EmployeeAccessScope,
): scope is Extract<EmployeeAccessScope, { values: readonly string[] }> {
  return isValuedScopeType(scope.type);
}

function isValuedScopeType(scopeType: string): scopeType is ValuedScopeType {
  return (
    scopeType === "business_unit" ||
    scopeType === "department" ||
    scopeType === "team" ||
    scopeType === "org_unit" ||
    scopeType === "org_unit_descendants" ||
    scopeType === "location" ||
    scopeType === "country" ||
    scopeType === "cost_center" ||
    scopeType === "legal_entity" ||
    scopeType === "project" ||
    scopeType === "assignment" ||
    scopeType === "workflow_instance"
  );
}

function normalizeFieldGroup(fieldGroup: string): EmployeeFieldGroup | undefined {
  if (fieldGroup === "emergency_contacts") {
    return "emergencyContacts";
  }

  if (SUPPORTED_EMPLOYEE_FIELD_GROUPS.includes(fieldGroup as EmployeeFieldGroup)) {
    return fieldGroup as EmployeeFieldGroup;
  }

  return undefined;
}

function employeeValuesForScope(
  scopeType: Exclude<
    PopulationScopeType,
    "self" | "direct_reports" | "manager_chain" | "global"
  >,
  employeeDocument: EmployeeProjectionDocument,
  indexedFields: Record<string, unknown>,
): string[] {
  const pathsByScope: Record<typeof scopeType, string[]> = {
    business_unit: [
      "businessUnit",
      "business_unit",
      "organization.businessUnit",
      "organization.business_unit",
      "job.businessUnit",
      "custom.businessUnit",
      "custom.business_unit",
    ],
    department: [
      "department",
      "organization.department",
      "job.department",
      "custom.department",
    ],
    team: ["team", "organization.team", "job.team", "custom.team"],
    org_unit: [
      "orgUnitId",
      "organization.orgUnitId",
      "organization.primaryTeamOrgUnitId",
      "organization.locationOrgUnitId",
      "organization.costCenterOrgUnitId",
      "custom.orgUnitId",
      "custom.primaryTeamOrgUnitId",
    ],
    org_unit_descendants: [
      "orgUnitId",
      "organization.orgUnitId",
      "organization.primaryTeamOrgUnitId",
      "organization.locationOrgUnitId",
      "organization.costCenterOrgUnitId",
      "custom.orgUnitId",
      "custom.primaryTeamOrgUnitId",
    ],
    location: [
      "location",
      "workLocation",
      "organization.location",
      "organization.locationOrgUnitId",
      "job.location",
      "custom.location",
      "custom.workLocation",
    ],
    country: [
      "country",
      "organization.country",
      "employment.country",
      "contact.homeAddress.country",
      "custom.country",
    ],
    cost_center: [
      "costCenter",
      "cost_center",
      "organization.costCenter",
      "organization.cost_center",
      "organization.costCenterOrgUnitId",
      "job.costCenter",
      "custom.costCenter",
      "custom.cost_center",
    ],
    legal_entity: [
      "legalEntity",
      "legal_entity",
      "organization.legalEntity",
      "employment.legalEntity",
      "employment.legal_entity",
      "custom.legalEntity",
      "custom.legal_entity",
    ],
    project: [
      "project",
      "projectId",
      "organization.project",
      "organization.projectId",
      "custom.project",
      "custom.projectId",
      "custom.projectIds",
    ],
    assignment: [
      "assignment",
      "assignmentId",
      "workerAssignmentId",
      "custom.assignment",
      "custom.assignmentId",
      "custom.workerAssignmentId",
    ],
    workflow_instance: [
      "workflowInstanceId",
      "workflow.workflowInstanceId",
      "custom.workflowInstanceId",
    ],
  };

  return valueListForPaths(employeeDocument, indexedFields, pathsByScope[scopeType]);
}

function valueListForPaths(
  employeeDocument: EmployeeProjectionDocument,
  indexedFields: Record<string, unknown>,
  paths: readonly string[],
): string[] {
  const values: string[] = [];

  for (const path of paths) {
    values.push(...stringValues(valueAtPath(employeeDocument, path)));
    values.push(...stringValues(valueAtPath(indexedFields, path)));
  }

  return values;
}

function stringValues(value: unknown): string[] {
  if (typeof value === "string" && value.length > 0) {
    return [value];
  }

  if (Array.isArray(value)) {
    return value.flatMap((item) => stringValues(item));
  }

  return [];
}

function valueAtPath(source: unknown, path: string): unknown {
  const pathSegments = path.split(".").filter((segment) => segment.length > 0);
  let currentValue = source;

  for (const pathSegment of pathSegments) {
    if (typeof currentValue !== "object" || currentValue === null) {
      return undefined;
    }

    currentValue = (currentValue as Record<string, unknown>)[pathSegment];
  }

  return currentValue;
}

function actionForFieldGroup(fieldGroup: EmployeeFieldGroup): string {
  const actionByFieldGroup: Record<EmployeeFieldGroup, string> = {
    profile: "employee.profile.view",
    organization: "employee.organization.view",
    job: "employee.job.view",
    employment: "employee.employment.view",
    compensation: "employee.compensation.view",
    contact: "employee.contact.view",
    emergencyContacts: "employee.emergency_contacts.view",
    workflow: "employee.workflow.view",
  };

  return actionByFieldGroup[fieldGroup];
}

function grantIdForGrant(
  grant: EmployeeAccessGrant | AccessGrantRecord,
): string | undefined {
  if ("accessGrantId" in grant) {
    return grant.accessGrantId;
  }

  return grant.grantId;
}

function scopeValuesForDecision(scope: EmployeeAccessScope): string[] {
  if ("values" in scope) {
    return [...scope.values];
  }

  return [];
}

function stringFromMetadata(
  metadata: Record<string, unknown>,
  key: string,
): string | undefined {
  const value = metadata[key];

  return typeof value === "string" ? value : undefined;
}

function maskEmployeeFieldGroup(
  employeeDocument: EmployeeDocumentRecord,
  fieldGroup: EmployeeFieldGroup,
): void {
  if (fieldGroup === "profile") {
    employeeDocument.person = {
      personId: MASKED_EMPLOYEE_FIELD_VALUE,
      legalName: {
        first: MASKED_EMPLOYEE_FIELD_VALUE,
        middle: null,
        last: MASKED_EMPLOYEE_FIELD_VALUE,
      },
      displayName: MASKED_EMPLOYEE_FIELD_VALUE,
      preferredName: null,
      workEmail: MASKED_EMPLOYEE_FIELD_VALUE,
    };
    return;
  }

  if (fieldGroup === "organization") {
    employeeDocument.manager = { employeeId: MASKED_EMPLOYEE_FIELD_VALUE };
    employeeDocument.organization = {
      legalEntity: MASKED_EMPLOYEE_FIELD_VALUE,
      businessUnit: MASKED_EMPLOYEE_FIELD_VALUE,
      department: MASKED_EMPLOYEE_FIELD_VALUE,
      team: MASKED_EMPLOYEE_FIELD_VALUE,
      location: MASKED_EMPLOYEE_FIELD_VALUE,
      payZone: MASKED_EMPLOYEE_FIELD_VALUE,
      costCenter: MASKED_EMPLOYEE_FIELD_VALUE,
    };
    return;
  }

  if (fieldGroup === "job") {
    employeeDocument.job = {
      jobCode: MASKED_EMPLOYEE_FIELD_VALUE,
      title: MASKED_EMPLOYEE_FIELD_VALUE,
      family: MASKED_EMPLOYEE_FIELD_VALUE,
      level: MASKED_EMPLOYEE_FIELD_VALUE,
    };
    return;
  }

  if (fieldGroup === "employment") {
    employeeDocument.employment = {
      status: MASKED_EMPLOYEE_FIELD_VALUE,
      legalEntity: MASKED_EMPLOYEE_FIELD_VALUE,
      hireDate: MASKED_EMPLOYEE_FIELD_VALUE,
      workerType: MASKED_EMPLOYEE_FIELD_VALUE,
    };
    return;
  }

  if (fieldGroup === "compensation") {
    employeeDocument.compensation = {
      amount: 0,
      currency: MASKED_EMPLOYEE_FIELD_VALUE,
      payFrequency: MASKED_EMPLOYEE_FIELD_VALUE,
      bonusTargetPercent: 0,
      effectiveDate: MASKED_EMPLOYEE_FIELD_VALUE,
    };
    return;
  }

  if (fieldGroup === "contact") {
    employeeDocument.contact = {
      personalEmail: null,
      mobilePhone: null,
      homeAddress: {
        line1: MASKED_EMPLOYEE_FIELD_VALUE,
        line2: null,
        city: MASKED_EMPLOYEE_FIELD_VALUE,
        region: MASKED_EMPLOYEE_FIELD_VALUE,
        postalCode: MASKED_EMPLOYEE_FIELD_VALUE,
        country: MASKED_EMPLOYEE_FIELD_VALUE,
      },
    };
    return;
  }

  if (fieldGroup === "emergencyContacts") {
    employeeDocument.emergencyContacts = [];
    return;
  }

  employeeDocument["workflow"] = MASKED_EMPLOYEE_FIELD_VALUE;
}

function maskEmployeeIndexedFields(
  indexedFields: Record<string, unknown>,
  fieldGroup: EmployeeFieldGroup,
): void {
  const indexedFieldKeysByGroup: Record<EmployeeFieldGroup, readonly string[]> = {
    profile: [
      "displayName",
      "firstName",
      "lastName",
      "legalName",
      "preferredName",
      "personId",
      "workEmail",
    ],
    organization: [
      "businessUnit",
      "business_unit",
      "department",
      "team",
      "location",
      "workLocation",
      "costCenter",
      "cost_center",
      "managerEmployeeId",
      "managerId",
      "organization",
      "orgUnitId",
      "primaryTeamOrgUnitId",
      "locationOrgUnitId",
      "costCenterOrgUnitId",
    ],
    job: ["jobCode", "jobLevel", "jobTitle", "jobFamily", "title", "level"],
    employment: ["employmentStatus", "legalEntity", "workerType", "hireDate"],
    compensation: [
      "compensation",
      "compensationAmount",
      "compensationCurrency",
      "compensationEffectiveDate",
      "salary",
      "salaryAmount",
      "payFrequency",
      "bonusTargetPercent",
      "annualCompensation",
    ],
    contact: [
      "personalEmail",
      "mobilePhone",
      "workPhone",
      "homeAddress",
      "homeCity",
      "homeRegion",
      "homePostalCode",
      "homeCountry",
      "postalCode",
      "address",
    ],
    emergencyContacts: [
      "emergencyContacts",
      "emergencyContactName",
      "emergencyContactPhone",
      "emergencyContactEmail",
    ],
    workflow: ["workflow", "workflowInstanceId", "changeRequestId"],
  };
  const keysToMask = new Set(indexedFieldKeysByGroup[fieldGroup]);

  for (const indexedFieldKey of Object.keys(indexedFields)) {
    if (keysToMask.has(indexedFieldKey)) {
      delete indexedFields[indexedFieldKey];
    }
  }
}

function maskWorkflowSummaryValue(
  value: unknown,
  allowedFieldGroups: ReadonlySet<EmployeeFieldGroup>,
): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => maskWorkflowSummaryValue(item, allowedFieldGroups));
  }

  if (typeof value !== "object" || value === null) {
    return value;
  }

  const maskedRecord: Record<string, unknown> = {};

  for (const [key, nestedValue] of Object.entries(value)) {
    const fieldGroup = fieldGroupForWorkflowSummaryKey(key);

    if (fieldGroup !== undefined && !allowedFieldGroups.has(fieldGroup)) {
      maskedRecord[key] = MASKED_EMPLOYEE_FIELD_VALUE;
      continue;
    }

    maskedRecord[key] = maskWorkflowSummaryValue(nestedValue, allowedFieldGroups);
  }

  return maskedRecord;
}

function fieldGroupForWorkflowSummaryKey(key: string): EmployeeFieldGroup | undefined {
  const normalizedKey = key.toLowerCase();

  if (
    normalizedKey.includes("compensation") ||
    normalizedKey.includes("salary") ||
    normalizedKey.includes("bonus") ||
    normalizedKey.includes("payfrequency")
  ) {
    return "compensation";
  }

  if (
    normalizedKey.includes("personalemail") ||
    normalizedKey.includes("mobilephone") ||
    normalizedKey.includes("homeaddress") ||
    normalizedKey.includes("contact")
  ) {
    return "contact";
  }

  if (normalizedKey.includes("emergency")) {
    return "emergencyContacts";
  }

  if (
    normalizedKey.includes("manager") ||
    normalizedKey.includes("organization") ||
    normalizedKey.includes("orgunit") ||
    normalizedKey.includes("location") ||
    normalizedKey.includes("costcenter") ||
    normalizedKey.includes("team")
  ) {
    return "organization";
  }

  if (
    normalizedKey.includes("job") ||
    normalizedKey.includes("title") ||
    normalizedKey.includes("level")
  ) {
    return "job";
  }

  return undefined;
}

function cloneProjection(
  projectionRecord: EmployeeProjectionRecord,
): EmployeeProjectionRecord {
  return JSON.parse(JSON.stringify(projectionRecord)) as EmployeeProjectionRecord;
}
