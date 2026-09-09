import {
  canonicalFieldTypeIds,
  type CanonicalFieldTypeId,
  type FieldTypeAliasDefinition,
} from "@human-capital-management-suite/ui-contracts";

type FieldRecord = Readonly<Record<string, unknown>>;

const canonicalFieldTypeSet = new Set<string>(canonicalFieldTypeIds);

const alias = (
  sourceType: string,
  canonicalType: CanonicalFieldTypeId,
  description: string,
  props: Readonly<Record<string, unknown>> = {},
  source: FieldTypeAliasDefinition["source"] = "legacy",
): FieldTypeAliasDefinition => ({
  sourceType,
  canonicalType,
  description,
  source,
  ...(Object.keys(props).length === 0 ? {} : { props }),
});

export const fieldTypeAliases: readonly FieldTypeAliasDefinition[] = [
  alias("email", "text", "Email is a text input with email semantics.", {
    inputType: "email",
    inputMode: "email",
    autoComplete: "email",
  }),
  alias("phone", "text", "Phone is a text input with telephone semantics.", {
    inputType: "tel",
    inputMode: "tel",
    transform: "phone_us",
  }),
  alias("url", "text", "URL is a text input with URL semantics.", {
    inputType: "url",
    inputMode: "url",
  }),
  alias("money", "number", "Money is a number input with currency formatting.", {
    format: "currency",
    inputMode: "decimal",
    prefix: "$",
  }),
  alias("percent", "number", "Percent is a number input with percentage formatting.", {
    format: "percent",
    inputMode: "decimal",
    suffix: "%",
  }),
  alias("date_range", "date", "Date ranges are composed from two date controls.", {
    composition: "start_end",
  }),
  alias("dropdown", "select", "Dropdown is the legacy name for select."),
  alias("grouped_select", "select", "Grouped select is a select with option groups.", {
    groupPath: "group",
  }),
  alias("dropdown_group", "select", "Dropdown group is a select with option groups.", {
    groupPath: "group",
  }),
  alias(
    "filterable_select",
    "combobox",
    "Filterable select is a searchable combobox.",
    {
      searchEnabled: true,
    },
  ),
  alias(
    "filterable_dropdown",
    "combobox",
    "Filterable dropdown is a searchable combobox.",
    {
      searchEnabled: true,
    },
  ),
  alias("radio", "radio_group", "Radio is normalized to radio group."),
  alias("toggle_group", "toggle", "Toggle groups are repeated toggle controls.", {
    repeated: true,
  }),
  alias("slider_group", "slider", "Slider groups are repeated slider controls.", {
    repeated: true,
  }),
  alias(
    "metric_slider_group",
    "slider",
    "Metric slider groups are repeated slider controls.",
    {
      repeated: true,
      metric: true,
    },
  ),
  alias("repeating_list", "repeater", "Repeating list is the legacy repeater ID."),
  alias("table_editor", "table", "Table editor is the legacy table field ID."),
  alias(
    "bulk_grid_editor",
    "table",
    "Bulk grid editor is a table recipe.",
    {},
    "compound_recipe",
  ),
  alias("matrix_editor", "matrix", "Matrix editor is the legacy matrix ID."),
  alias("file", "file_upload", "File is the legacy upload field ID."),
  alias("evidence_upload", "file_upload", "Evidence upload is a file upload recipe.", {
    purpose: "evidence",
  }),
  alias("e_signature", "signature", "E-signature is normalized to signature."),
  alias(
    "signature_capture",
    "signature",
    "Signature capture is normalized to signature.",
  ),
  alias(
    "sensitive_field_reveal",
    "sensitive_reveal",
    "Sensitive field reveal is normalized to sensitive reveal.",
  ),
  alias("read_only", "readonly", "Read-only value uses the readonly field control."),
  alias(
    "readonly_value",
    "readonly",
    "Read-only value uses the readonly field control.",
  ),
];

const entityPickerTypes: Readonly<Record<string, string>> = {
  employee_picker: "employee",
  manager_picker: "employee",
  org_unit_picker: "org_unit",
  department_picker: "department",
  legal_entity_picker: "legal_entity",
  cost_center_picker: "cost_center",
  location_picker: "location",
  job_profile_picker: "job_profile",
  position_picker: "position",
  pay_band_picker: "pay_band",
  payroll_cutoff_picker: "payroll_cutoff",
  permission_entity_picker: "permission_entity",
};

const treePickerTypes: Readonly<Record<string, string>> = {
  manager_tree_picker: "manager",
  org_tree_picker: "org",
};

const compoundRecipeTypes: Readonly<Record<string, CanonicalFieldTypeId>> = {
  compensation_editor: "table",
  job_change_editor: "repeater",
  manager_org_change_editor: "repeater",
  worker_assignment_editor: "repeater",
  role_binding_editor: "table",
  effective_dated_fact_editor: "repeater",
  effective_dated_change: "repeater",
  before_after_field_editor: "repeater",
  compensation_package_editor: "table",
  schedule_time_control: "repeater",
  international_contact: "repeater",
  approval_chain_editor: "repeater",
  policy_evidence_checklist: "repeater",
  ai_review_panel: "repeater",
  conflict_resolver: "table",
  integration_repair_control: "repeater",
  transaction_simulation_viewer: "repeater",
  cluster_board: "repeater",
  drag_drop_clusters: "repeater",
  policy_acknowledgement: "checkbox",
};

const fieldTypeAliasMap = new Map(
  fieldTypeAliases.map((entry) => [entry.sourceType, entry]),
);

const dynamicFieldAlias = (fieldType: string): FieldTypeAliasDefinition | undefined => {
  const entityType = entityPickerTypes[fieldType];

  if (entityType !== undefined) {
    return alias(
      fieldType,
      "entity_picker",
      "Domain picker controls normalize to entity picker with entityType props.",
      { entityType },
      "domain",
    );
  }

  const treeType = treePickerTypes[fieldType];

  if (treeType !== undefined) {
    return alias(
      fieldType,
      "tree_picker",
      "Tree picker controls normalize to tree picker with treeType props.",
      { treeType },
      "domain",
    );
  }

  const compoundType = compoundRecipeTypes[fieldType];

  if (compoundType !== undefined) {
    return alias(
      fieldType,
      compoundType,
      "Compound controls normalize to generated field recipes.",
      { recipeType: fieldType },
      "compound_recipe",
    );
  }

  return undefined;
};

const isFieldRecord = (value: unknown): value is FieldRecord =>
  typeof value === "object" && value !== null && !Array.isArray(value);

export const isCanonicalFieldType = (
  fieldType: string,
): fieldType is CanonicalFieldTypeId => canonicalFieldTypeSet.has(fieldType);

export const findFieldTypeAlias = (
  fieldType: string,
): FieldTypeAliasDefinition | undefined =>
  fieldTypeAliasMap.get(fieldType) ?? dynamicFieldAlias(fieldType);

export const resolveCanonicalFieldType = (
  fieldType: string,
): CanonicalFieldTypeId | string => {
  if (isCanonicalFieldType(fieldType)) {
    return fieldType;
  }

  return findFieldTypeAlias(fieldType)?.canonicalType ?? fieldType;
};

export const normalizeGeneratedField = (field: FieldRecord): FieldRecord => {
  const sourceType = typeof field.type === "string" ? field.type : "text";
  const canonicalType = resolveCanonicalFieldType(sourceType);
  const fieldAlias = findFieldTypeAlias(sourceType);

  if (fieldAlias === undefined && canonicalType === sourceType) {
    return field;
  }

  return {
    ...(fieldAlias?.props ?? {}),
    ...field,
    type: canonicalType,
    canonicalType,
    originalType: sourceType,
  };
};

export const normalizeGeneratedFieldsInProps = (
  props: Readonly<Record<string, unknown>> | undefined,
): Readonly<Record<string, unknown>> | undefined => {
  if (props === undefined || !Array.isArray(props.fields)) {
    return props;
  }

  return {
    ...props,
    fields: props.fields.map((field) =>
      isFieldRecord(field) ? normalizeGeneratedField(field) : field,
    ),
  };
};
