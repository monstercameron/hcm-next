export type RuleSource =
  | "actor"
  | "workflow"
  | "employee"
  | "tenant"
  | "brand"
  | "surface"
  | "binding";

export type RuleOperator =
  | "equals"
  | "not_equals"
  | "includes"
  | "exists"
  | "in_surface"
  | "has_permission"
  | "matches_role";

export type RuleDefinition = {
  id: string;
  source: RuleSource;
  operator: RuleOperator;
  path?: string;
  value?: unknown;
};

export type RuleSet = {
  all?: readonly RuleDefinition[];
  any?: readonly RuleDefinition[];
  not?: readonly RuleDefinition[];
};
