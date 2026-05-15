export type BindingSourceType =
  | "constant"
  | "workflow_input"
  | "workflow_context"
  | "workflow_state"
  | "workflow_config"
  | "employee_projection"
  | "actor_profile"
  | "tenant_config"
  | "brand_pack"
  | "api"
  | "external_integration"
  | "computed"
  | "previous_workflow_response"
  | "manual_override"
  | "uploaded_asset";

export type BindingConfidence = "authoritative" | "derived" | "suggested" | "unknown";

export type DataBinding = {
  source: BindingSourceType;
  path?: string;
  value?: unknown;
  fallback?: unknown;
  params?: Readonly<Record<string, DataBinding>>;
  transform?: string;
  label?: string;
};

export type BoundValue<T = unknown> = {
  value: T;
  source: BindingSourceType;
  sourcePath?: string;
  confidence: BindingConfidence;
  editable: boolean;
  resolved: boolean;
  lastResolvedAt?: string;
};
