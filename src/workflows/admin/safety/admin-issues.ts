export type WorkflowAdminIssueSeverity = "error" | "warning";

export type WorkflowAdminIssue = {
  severity: WorkflowAdminIssueSeverity;
  code: string;
  path: string;
  message: string;
  suggestedRepair?: string;
  details?: Record<string, unknown>;
};

export type WorkflowAdminValidationReport = {
  valid: boolean;
  errors: WorkflowAdminIssue[];
  warnings: WorkflowAdminIssue[];
};

export type WorkflowAdminRiskLevel = "low" | "medium" | "high";

/**
 * Creates a typed admin validation error with a suggested repair.
 */
export function adminError(input: {
  code: string;
  path: string;
  message: string;
  suggestedRepair?: string;
  details?: Record<string, unknown>;
}): WorkflowAdminIssue {
  return adminIssue({ ...input, severity: "error" });
}

/**
 * Creates a typed admin validation warning with a suggested repair.
 */
export function adminWarning(input: {
  code: string;
  path: string;
  message: string;
  suggestedRepair?: string;
  details?: Record<string, unknown>;
}): WorkflowAdminIssue {
  return adminIssue({ ...input, severity: "warning" });
}

/**
 * Splits issues into the common admin validation report shape.
 */
export function workflowAdminValidationReport(
  issues: WorkflowAdminIssue[],
): WorkflowAdminValidationReport {
  const errors = issues.filter((issue) => issue.severity === "error");
  const warnings = issues.filter((issue) => issue.severity === "warning");

  return {
    valid: errors.length === 0,
    errors,
    warnings,
  };
}

function adminIssue(input: {
  severity: WorkflowAdminIssueSeverity;
  code: string;
  path: string;
  message: string;
  suggestedRepair?: string;
  details?: Record<string, unknown>;
}): WorkflowAdminIssue {
  return {
    severity: input.severity,
    code: input.code,
    path: input.path,
    message: input.message,
    ...(input.suggestedRepair !== undefined
      ? { suggestedRepair: input.suggestedRepair }
      : {}),
    ...(input.details !== undefined ? { details: input.details } : {}),
  };
}
