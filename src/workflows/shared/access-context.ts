import { ok, type AppError, type Result } from "@hcm-next/foundation";
import type { AccessGrantRecord, ActorRecord } from "@hcm-next/data-store";
import type { AppDependencies } from "../../api/dependencies.js";
import type {
  EmployeeAccessGrant,
  EmployeeAccessGrants,
  EmployeeAccessInput,
  EmployeeAccessScope,
  EmployeeFieldGroup as EmployeeAccessFieldGroup,
} from "./employee-access.js";

/**
 * Builds the full employee-access context needed by projection and timeline reads.
 */
export function findAccessGrantsForActor(
  dependencies: AppDependencies,
  actor: ActorRecord,
): Result<EmployeeAccessInput, AppError> {
  const accessGrantsResult = dependencies.repositories.accessGrants.findActiveForActor(
    actor.tenantId,
    actor.actorId,
  );

  if (!accessGrantsResult.ok) {
    return accessGrantsResult;
  }

  const roleBindingsResult = dependencies.repositories.roleBindings.findActiveForActor(
    actor.tenantId,
    actor.actorId,
  );
  if (!roleBindingsResult.ok) {
    return roleBindingsResult;
  }

  const workerAssignmentsResult =
    dependencies.repositories.workerAssignments.findActiveByTenant(actor.tenantId);
  if (!workerAssignmentsResult.ok) {
    return workerAssignmentsResult;
  }

  const organizationRelationshipsResult =
    dependencies.repositories.organizationRelationships.findByTenant(actor.tenantId);
  if (!organizationRelationshipsResult.ok) {
    return organizationRelationshipsResult;
  }

  const organizationUnitsResult =
    dependencies.repositories.organizationUnits.findByTenant(actor.tenantId);
  if (!organizationUnitsResult.ok) {
    return organizationUnitsResult;
  }

  return ok({
    legacyGrants: toEmployeeAccessGrants(accessGrantsResult.value),
    roleBindings: roleBindingsResult.value,
    workerAssignments: workerAssignmentsResult.value,
    organizationRelationships: organizationRelationshipsResult.value,
    organizationUnits: organizationUnitsResult.value,
  });
}

function toEmployeeAccessGrants(
  accessGrantRecords: AccessGrantRecord[],
): EmployeeAccessGrants {
  return accessGrantRecords.map((accessGrantRecord): EmployeeAccessGrant => {
    return {
      grantId: accessGrantRecord.accessGrantId,
      actorIds: [accessGrantRecord.actorId],
      fieldGroups: accessGrantRecord.fieldGroups.map(toEmployeeAccessFieldGroup),
      scopes: [toEmployeeAccessScope(accessGrantRecord.scope)],
    };
  });
}

function toEmployeeAccessFieldGroup(
  fieldGroup: AccessGrantRecord["fieldGroups"][number],
): EmployeeAccessFieldGroup {
  if (fieldGroup === "emergency_contacts") {
    return "emergencyContacts";
  }

  return fieldGroup;
}

function toEmployeeAccessScope(scope: AccessGrantRecord["scope"]): EmployeeAccessScope {
  if (
    scope.type === "business_unit" ||
    scope.type === "department" ||
    scope.type === "team" ||
    scope.type === "location" ||
    scope.type === "cost_center" ||
    scope.type === "legal_entity"
  ) {
    return {
      type: scope.type,
      values: scope.values ?? [],
    };
  }

  return {
    type: scope.type,
  };
}
