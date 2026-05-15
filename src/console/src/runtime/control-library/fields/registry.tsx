import { BasicInputControl, ReadOnlyValueControl } from "./basic-inputs";
import { ChoiceControl } from "./choice-controls";
import { FileSignatureControl } from "./file-signature-controls";
import { GovernanceControl } from "./governance-controls";
import { HcmControl } from "./hcm-controls";
import { SimulationRepairControl } from "./simulation-repair-controls";
import { StructuredControl } from "./structured-controls";
import { ToggleSliderControl } from "./toggle-slider-controls";
import type {
  FieldControlCategory,
  FieldControlComponent,
  FieldControlProps,
  FieldControlRegistryEntry,
} from "./types";

const basicControlTypes = [
  "text",
  "textarea",
  "number",
  "money",
  "percent",
  "date",
  "time",
  "email",
  "phone",
  "url",
  "date_range",
] as const;

const choiceControlTypes = [
  "select",
  "dropdown",
  "grouped_select",
  "dropdown_group",
  "filterable_select",
  "filterable_dropdown",
  "multi_select",
  "radio",
  "radio_group",
  "checkbox",
] as const;

const toggleControlTypes = [
  "toggle",
  "toggle_group",
  "slider",
  "slider_group",
  "metric_slider_group",
] as const;

const structuredControlTypes = [
  "repeating_list",
  "table_editor",
  "matrix",
  "cluster_board",
  "drag_drop_clusters",
] as const;

const fileControlTypes = [
  "file",
  "evidence_upload",
  "policy_acknowledgement",
  "signature",
  "e_signature",
  "signature_capture",
] as const;

const hcmControlTypes = [
  "employee_picker",
  "manager_picker",
  "org_unit_picker",
  "department_picker",
  "legal_entity_picker",
  "cost_center_picker",
  "location_picker",
  "job_profile_picker",
  "position_picker",
  "pay_band_picker",
  "payroll_cutoff_picker",
  "permission_entity_picker",
  "manager_tree_picker",
  "org_tree_picker",
  "compensation_editor",
  "job_change_editor",
  "manager_org_change_editor",
  "worker_assignment_editor",
  "role_binding_editor",
  "effective_dated_fact_editor",
  "effective_dated_change",
  "before_after_field_editor",
  "compensation_package_editor",
  "schedule_time_control",
  "international_contact",
] as const;

const governanceControlTypes = [
  "approval_chain_editor",
  "policy_evidence_checklist",
  "ai_review_panel",
  "sensitive_field_reveal",
] as const;

const simulationControlTypes = [
  "bulk_grid_editor",
  "conflict_resolver",
  "integration_repair_control",
  "transaction_simulation_viewer",
] as const;

const labelFromType = (type: string): string =>
  type
    .split("_")
    .map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(" ");

const entriesForTypes = (
  types: readonly string[],
  category: FieldControlCategory,
  component: FieldControlComponent,
): readonly FieldControlRegistryEntry[] =>
  types.map((type) => ({
    type,
    label: labelFromType(type),
    category,
    component,
  }));

export const fieldControlRegistryEntries: readonly FieldControlRegistryEntry[] = [
  ...entriesForTypes(basicControlTypes, "basic", BasicInputControl),
  ...entriesForTypes(choiceControlTypes, "choice", ChoiceControl),
  ...entriesForTypes(toggleControlTypes, "toggle", ToggleSliderControl),
  ...entriesForTypes(structuredControlTypes, "structured", StructuredControl),
  ...entriesForTypes(fileControlTypes, "files", FileSignatureControl),
  ...entriesForTypes(hcmControlTypes, "hcm", HcmControl),
  ...entriesForTypes(governanceControlTypes, "governance", GovernanceControl),
  ...entriesForTypes(simulationControlTypes, "simulation", SimulationRepairControl),
];

export const fieldControlRegistry: Readonly<Record<string, FieldControlComponent>> =
  Object.fromEntries(
    fieldControlRegistryEntries.map((entry) => [entry.type, entry.component]),
  );

/** Returns the registered component for a generated field control type. */
export const getFieldControlComponent = (type: string): FieldControlComponent =>
  fieldControlRegistry[type] ?? ReadOnlyValueControl;

/** Renders a generated field control using the registry component map. */
export const FieldControlFactory = (props: FieldControlProps) => {
  const Component = getFieldControlComponent(props.config.type);

  return <Component {...props} />;
};

export const fieldControlTypesByCategory: Readonly<
  Record<FieldControlCategory, readonly string[]>
> = {
  basic: basicControlTypes,
  choice: choiceControlTypes,
  toggle: toggleControlTypes,
  structured: structuredControlTypes,
  files: fileControlTypes,
  hcm: hcmControlTypes,
  governance: governanceControlTypes,
  simulation: simulationControlTypes,
};
