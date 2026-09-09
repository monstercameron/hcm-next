import type {
  BoundValue,
  BindingConfidence,
  DataBinding,
  WidgetInstance,
} from "@human-capital-management-suite/ui-contracts";
import type { UiRuntimeContext } from "./context";
import { readPath } from "./object-path";

const confidenceForSource = (source: DataBinding["source"]): BindingConfidence => {
  if (
    source === "workflow_state" ||
    source === "workflow_context" ||
    source === "employee_projection" ||
    source === "actor_profile" ||
    source === "tenant_config" ||
    source === "brand_pack"
  ) {
    return "authoritative";
  }

  if (source === "computed" || source === "previous_workflow_response") {
    return "derived";
  }

  if (source === "api" || source === "external_integration") {
    return "suggested";
  }

  return "unknown";
};

const sourceRoot = (binding: DataBinding, context: UiRuntimeContext): unknown => {
  switch (binding.source) {
    case "constant":
      return binding.value;
    case "workflow_input":
      return context.workflow.input;
    case "workflow_context":
      return context.workflow.context;
    case "workflow_state":
      return context.workflow;
    case "workflow_config":
      return context.workflow.config;
    case "employee_projection":
      return context.employee;
    case "actor_profile":
      return context.actor;
    case "tenant_config":
      return context.tenant.config;
    case "brand_pack":
      return context.brand;
    case "api":
      return context.apiData;
    case "external_integration":
      return context.integrationData;
    case "computed":
      return context.manualValues;
    case "previous_workflow_response":
      return context.previousWorkflowResponses;
    case "manual_override":
      return context.manualValues;
    case "uploaded_asset":
      return context.uploadedAssets;
  }
};

export const resolveBinding = (
  binding: DataBinding,
  context: UiRuntimeContext,
): BoundValue => {
  const rawValue =
    binding.source === "constant"
      ? binding.value
      : readPath(sourceRoot(binding, context), binding.path);
  const resolved = rawValue !== undefined;
  const value = resolved ? rawValue : binding.fallback;

  const boundValue: BoundValue = {
    value,
    source: binding.source,
    confidence: confidenceForSource(binding.source),
    editable:
      binding.source === "manual_override" || binding.source === "workflow_input",
    resolved: value !== undefined,
    lastResolvedAt: context.nowIso,
  };

  return binding.path === undefined
    ? boundValue
    : {
        ...boundValue,
        sourcePath: binding.path,
      };
};

export const resolveWidgetBindings = (
  widget: WidgetInstance,
  context: UiRuntimeContext,
): Readonly<Record<string, BoundValue>> => {
  const resolved: Record<string, BoundValue> = {};

  for (const [bindingName, binding] of Object.entries(widget.bindings ?? {})) {
    resolved[bindingName] = resolveBinding(binding, context);
  }

  return resolved;
};
