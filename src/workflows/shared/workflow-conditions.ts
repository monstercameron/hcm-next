import { valueAtDotPath } from "./json-fields.js";
import type {
  WorkflowAtomicOutcomeCondition,
  WorkflowOutcomeCondition,
  WorkflowTemplateSources,
} from "./workflow-config.js";

export type WorkflowConditionSources = WorkflowTemplateSources & {
  externalWriteError?: Record<string, unknown> | undefined;
};

/**
 * Evaluates declarative workflow outcome conditions against runtime sources.
 */
export function workflowConditionMatches(
  condition: WorkflowOutcomeCondition,
  sources: WorkflowConditionSources,
): boolean {
  if (isAllCondition(condition)) {
    return condition.all.every((childCondition) =>
      workflowConditionMatches(childCondition, sources),
    );
  }

  if (isAnyCondition(condition)) {
    return condition.any.some((childCondition) =>
      workflowConditionMatches(childCondition, sources),
    );
  }

  if (isNotCondition(condition)) {
    return !workflowConditionMatches(condition.not, sources);
  }

  return atomicConditionMatches(condition, sources);
}

function atomicConditionMatches(
  condition: WorkflowAtomicOutcomeCondition,
  sources: WorkflowConditionSources,
): boolean {
  const sourceValue =
    condition.$source === "externalWriteError"
      ? sources.externalWriteError
      : sources[condition.$source];
  const comparisonValue =
    typeof sourceValue === "object" && sourceValue !== null
      ? valueAtDotPath(sourceValue as Record<string, unknown>, condition.path)
      : undefined;

  if (condition.exists !== undefined) {
    const exists = comparisonValue !== undefined;

    if (exists !== condition.exists) {
      return false;
    }
  }

  if ("equals" in condition && !Object.is(comparisonValue, condition.equals)) {
    return false;
  }

  if ("notEquals" in condition && Object.is(comparisonValue, condition.notEquals)) {
    return false;
  }

  if (
    condition.in !== undefined &&
    !condition.in.some((allowedValue) => Object.is(allowedValue, comparisonValue))
  ) {
    return false;
  }

  if (
    condition.notIn !== undefined &&
    condition.notIn.some((blockedValue) => Object.is(blockedValue, comparisonValue))
  ) {
    return false;
  }

  return true;
}

function isAllCondition(
  condition: WorkflowOutcomeCondition,
): condition is { all: WorkflowOutcomeCondition[] } {
  return "all" in condition;
}

function isAnyCondition(
  condition: WorkflowOutcomeCondition,
): condition is { any: WorkflowOutcomeCondition[] } {
  return "any" in condition;
}

function isNotCondition(
  condition: WorkflowOutcomeCondition,
): condition is { not: WorkflowOutcomeCondition } {
  return "not" in condition;
}
