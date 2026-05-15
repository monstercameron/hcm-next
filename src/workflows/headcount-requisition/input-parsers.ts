import {
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import { numberField, stringArrayField, stringField } from "../shared/json-fields.js";
import type { HeadcountApprovalDecisionInput, HeadcountInput } from "./types.js";

/**
 * Parses the intake payload for the headcount requisition workflow.
 */
export function parseHeadcountInput(
  input: Record<string, unknown>,
): Result<HeadcountInput, AppError> {
  const department = stringField(input, "department");
  const team = stringField(input, "team");
  const location = stringField(input, "location");
  const costCenter = stringField(input, "costCenter");
  const jobCode = stringField(input, "jobCode");
  const title = stringField(input, "title");
  const level = stringField(input, "level");
  const requestedFte = numberField(input, "requestedFte");
  const targetStartDate = stringField(input, "targetStartDate");
  const salaryRangeMin = numberField(input, "salaryRangeMin");
  const salaryRangeMax = numberField(input, "salaryRangeMax");
  const businessJustification = stringField(input, "businessJustification");
  const selectedLeadershipApprovers = stringArrayField(
    input,
    "selectedLeadershipApprovers",
  );

  if (
    department === undefined ||
    team === undefined ||
    location === undefined ||
    costCenter === undefined ||
    jobCode === undefined ||
    title === undefined ||
    level === undefined ||
    requestedFte === undefined ||
    requestedFte <= 0 ||
    targetStartDate === undefined ||
    salaryRangeMin === undefined ||
    salaryRangeMax === undefined ||
    salaryRangeMin > salaryRangeMax ||
    businessJustification === undefined ||
    selectedLeadershipApprovers.length === 0
  ) {
    return err(
      validationFailedError({
        department,
        team,
        location,
        costCenter,
        jobCode,
        title,
        level,
        requestedFte,
        targetStartDate,
        salaryRangeMin,
        salaryRangeMax,
        businessJustification,
        selectedLeadershipApprovers,
      }),
    );
  }

  if (
    new Set(selectedLeadershipApprovers).size !== selectedLeadershipApprovers.length
  ) {
    return err(
      validationFailedError({
        selectedLeadershipApprovers: "duplicate_actor_ids",
      }),
    );
  }

  return ok({
    department,
    team,
    location,
    costCenter,
    jobCode,
    title,
    level,
    requestedFte,
    targetStartDate,
    salaryRangeMin,
    salaryRangeMax,
    businessJustification,
    selectedLeadershipApprovers,
  });
}

/**
 * Parses an approval task decision payload with optional optimistic task version.
 */
export function parseHeadcountApprovalDecisionInput(
  input: Record<string, unknown>,
): Result<HeadcountApprovalDecisionInput, AppError> {
  const approvalTaskId = stringField(input, "approvalTaskId");
  const taskVersion =
    numberField(input, "taskVersion") ?? numberField(input, "expectedTaskVersion");
  const comment = stringField(input, "comment");
  const reason = stringField(input, "reason");

  if (approvalTaskId === undefined || taskVersion === undefined) {
    return err(validationFailedError({ approvalTaskId, taskVersion }));
  }

  return ok({
    approvalTaskId,
    taskVersion,
    ...(comment !== undefined ? { comment } : {}),
    ...(reason !== undefined ? { reason } : {}),
  });
}
