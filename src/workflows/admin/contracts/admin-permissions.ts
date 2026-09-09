import {
  ACTOR_ROLES,
  err,
  ok,
  permissionDeniedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type { ActorRecord } from "@human-capital-management-suite/data-store";

const workflowAdminRoles = new Set<string>([
  ACTOR_ROLES.HR_ADMIN,
  ACTOR_ROLES.FINANCE_ADMIN,
  ACTOR_ROLES.COMPENSATION_ADMIN,
  ACTOR_ROLES.SYSTEM,
]);

/**
 * Enforces the temporary v0 workflow-admin role boundary.
 */
export function requireWorkflowAdmin(actor: ActorRecord): Result<true, AppError> {
  const isWorkflowAdmin = actor.roles.some((role) => workflowAdminRoles.has(role));

  if (!isWorkflowAdmin) {
    return err(permissionDeniedError({ permission: "workflow_admin" }));
  }

  return ok(true);
}
