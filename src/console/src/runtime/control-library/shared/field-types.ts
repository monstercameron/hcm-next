import { isControlRecord, stringValue } from "./guards";
import type { ControlRecord, FieldControlConfig } from "./types";

export const canonicalFieldControlTypes = [
  "text",
  "textarea",
  "number",
  "date",
  "time",
  "select",
  "combobox",
  "multi_select",
  "radio_group",
  "checkbox",
  "toggle",
  "slider",
  "repeater",
  "table",
  "matrix",
  "entity_picker",
  "tree_picker",
  "file_upload",
  "signature",
  "sensitive_reveal",
  "readonly",
] as const;

export type CanonicalFieldControlType = (typeof canonicalFieldControlTypes)[number];

const canonicalFieldControlTypeSet = new Set<string>(canonicalFieldControlTypes);

export const isCanonicalFieldControlType = (
  type: string,
): type is CanonicalFieldControlType => canonicalFieldControlTypeSet.has(type);

export const fieldControlTypeAliases = {
  money: "number",
  percent: "number",
  email: "text",
  phone: "text",
  url: "text",
  date_range: "date",
  dropdown: "select",
  grouped_select: "select",
  dropdown_group: "select",
  filterable_select: "combobox",
  filterable_dropdown: "combobox",
  radio: "radio_group",
  toggle_group: "toggle",
  slider_group: "slider",
  metric_slider_group: "slider",
  repeating_list: "repeater",
  table_editor: "table",
  bulk_grid_editor: "table",
  cluster_board: "table",
  drag_drop_clusters: "table",
  file: "file_upload",
  evidence_upload: "file_upload",
  policy_acknowledgement: "checkbox",
  e_signature: "signature",
  signature_capture: "signature",
  employee_picker: "entity_picker",
  manager_picker: "entity_picker",
  org_unit_picker: "entity_picker",
  department_picker: "entity_picker",
  legal_entity_picker: "entity_picker",
  cost_center_picker: "entity_picker",
  location_picker: "entity_picker",
  job_profile_picker: "entity_picker",
  position_picker: "entity_picker",
  pay_band_picker: "entity_picker",
  payroll_cutoff_picker: "entity_picker",
  permission_entity_picker: "entity_picker",
  manager_tree_picker: "tree_picker",
  org_tree_picker: "tree_picker",
  compensation_editor: "readonly",
  job_change_editor: "readonly",
  manager_org_change_editor: "readonly",
  worker_assignment_editor: "readonly",
  role_binding_editor: "readonly",
  effective_dated_fact_editor: "readonly",
  effective_dated_change: "readonly",
  before_after_field_editor: "readonly",
  compensation_package_editor: "readonly",
  schedule_time_control: "readonly",
  international_contact: "readonly",
  approval_chain_editor: "readonly",
  policy_evidence_checklist: "readonly",
  ai_review_panel: "readonly",
  sensitive_field_reveal: "sensitive_reveal",
  conflict_resolver: "readonly",
  integration_repair_control: "readonly",
  transaction_simulation_viewer: "readonly",
} as const satisfies Readonly<Record<string, CanonicalFieldControlType>>;

export type FieldControlAliasType = keyof typeof fieldControlTypeAliases;

const aliasDefaults = {
  email: {
    inputType: "email",
    inputMode: "email",
    autoComplete: "email",
  },
  phone: {
    inputType: "tel",
    inputMode: "tel",
    autoComplete: "tel",
  },
  url: {
    inputType: "url",
    inputMode: "url",
  },
  money: {
    inputType: "number",
    format: "currency",
    prefix: "$",
    step: 0.01,
  },
  percent: {
    inputType: "number",
    format: "percent",
    suffix: "%",
    min: 0,
    max: 100,
    step: 1,
  },
  grouped_select: {
    grouped: true,
  },
  dropdown_group: {
    grouped: true,
  },
  filterable_select: {
    behavior: {
      searchEnabled: true,
    },
  },
  filterable_dropdown: {
    behavior: {
      searchEnabled: true,
    },
  },
} as const satisfies Readonly<Record<string, ControlRecord>>;

const nestedConfigKeys = [
  "data",
  "display",
  "style",
  "behavior",
  "validation",
] as const;

const mergeNestedRecord = (
  baseRecord: ControlRecord,
  overrideRecord: ControlRecord,
  key: (typeof nestedConfigKeys)[number],
): ControlRecord | undefined => {
  const baseValue = baseRecord[key];
  const overrideValue = overrideRecord[key];

  if (!isControlRecord(baseValue) && !isControlRecord(overrideValue)) {
    return undefined;
  }

  return {
    ...(isControlRecord(baseValue) ? baseValue : {}),
    ...(isControlRecord(overrideValue) ? overrideValue : {}),
  };
};

export const canonicalFieldControlTypeFor = (
  type: string,
): CanonicalFieldControlType =>
  isCanonicalFieldControlType(type)
    ? type
    : (fieldControlTypeAliases[type as FieldControlAliasType] ?? "readonly");

export const fieldControlSourceType = (config: FieldControlConfig): string =>
  stringValue(config.raw.sourceType, config.type);

/**
 * Normalizes compatibility field IDs into canonical IDs while preserving the
 * source type and alias defaults for the renderer components.
 */
export const normalizeFieldControlRecord = (field: ControlRecord): ControlRecord => {
  const sourceType = stringValue(field.type, "text");
  const type = canonicalFieldControlTypeFor(sourceType);
  const defaults = aliasDefaults[sourceType as keyof typeof aliasDefaults] ?? {};
  const nestedDefaults = Object.fromEntries(
    nestedConfigKeys.flatMap((key) => {
      const value = mergeNestedRecord(defaults, field, key);

      return value === undefined ? [] : [[key, value]];
    }),
  );

  return {
    ...defaults,
    ...field,
    ...nestedDefaults,
    sourceType,
    type,
  };
};

export const normalizeFieldControlConfig = (
  config: FieldControlConfig,
): FieldControlConfig => {
  const normalizedRaw = normalizeFieldControlRecord({
    ...config.raw,
    type: stringValue(config.raw.sourceType, config.type),
  });
  const type = canonicalFieldControlTypeFor(
    stringValue(normalizedRaw.type, config.type),
  );

  return {
    ...config,
    type,
    raw: normalizedRaw,
  };
};
