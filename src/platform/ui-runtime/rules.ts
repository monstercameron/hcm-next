import type { BoundValue, RuleDefinition, RuleSet } from "@human-capital-management-suite/ui-contracts";
import type { UiRuntimeContext } from "./context";
import { readPath } from "./object-path";

type RuleContext = {
  runtime: UiRuntimeContext;
  bindings?: Readonly<Record<string, BoundValue>>;
};

const sourceValue = (rule: RuleDefinition, context: RuleContext): unknown => {
  switch (rule.source) {
    case "actor":
      return readPath(context.runtime.actor, rule.path);
    case "workflow":
      return readPath(context.runtime.workflow, rule.path);
    case "employee":
      return readPath(context.runtime.employee, rule.path);
    case "tenant":
      return readPath(context.runtime.tenant, rule.path);
    case "brand":
      return readPath(context.runtime.brand, rule.path);
    case "surface":
      return context.runtime.surfaceMode;
    case "binding":
      return readPath(context.bindings, rule.path);
  }
};

const arrayIncludes = (container: readonly unknown[], value: unknown): boolean =>
  container.some((item) => item === value);

export const evaluateRule = (rule: RuleDefinition, context: RuleContext): boolean => {
  const value = sourceValue(rule, context);

  switch (rule.operator) {
    case "equals":
      return value === rule.value;
    case "not_equals":
      return value !== rule.value;
    case "exists":
      return value !== undefined && value !== null;
    case "includes":
      return Array.isArray(value)
        ? arrayIncludes(value, rule.value)
        : typeof value === "string" && typeof rule.value === "string"
          ? value.includes(rule.value)
          : false;
    case "in_surface":
      return Array.isArray(rule.value)
        ? arrayIncludes(rule.value, context.runtime.surfaceMode)
        : context.runtime.surfaceMode === rule.value;
    case "has_permission":
      return typeof rule.value === "string"
        ? context.runtime.actor.permissions.includes(rule.value)
        : false;
    case "matches_role":
      return typeof rule.value === "string"
        ? context.runtime.actor.roles.includes(rule.value)
        : false;
  }

  return false;
};

export const evaluateRuleSet = (
  ruleSet: RuleSet | undefined,
  runtime: UiRuntimeContext,
  bindings?: Readonly<Record<string, BoundValue>>,
): boolean => {
  if (ruleSet === undefined) {
    return true;
  }

  const context: RuleContext =
    bindings === undefined ? { runtime } : { runtime, bindings };
  const allRulesPass = (ruleSet.all ?? []).every((rule) => evaluateRule(rule, context));
  const anyRules = ruleSet.any ?? [];
  const anyRulesPass =
    anyRules.length === 0 || anyRules.some((rule) => evaluateRule(rule, context));
  const notRulesPass = (ruleSet.not ?? []).every(
    (rule) => !evaluateRule(rule, context),
  );

  return allRulesPass && anyRulesPass && notRulesPass;
};
