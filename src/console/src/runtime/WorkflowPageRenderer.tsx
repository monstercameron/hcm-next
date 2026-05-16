import type { PageDefinition } from "@hcm-next/ui-contracts";
import { resolvePage, type ResolvedWidget } from "@hcm-next/ui-runtime";
import type { UiRuntimeContext } from "@hcm-next/ui-runtime";
import { CheckCircle2, CircleAlert, CircleHelp, CircleX } from "lucide-react";
import {
  useEffect,
  useMemo,
  useState,
  type DragEvent,
  type InputHTMLAttributes,
  type PointerEvent,
  type TextareaHTMLAttributes,
} from "react";
import {
  controlClassName,
  controlStyleVariables,
  createFieldControlConfig,
  normalizeControlRecords,
  type FieldControlConfig,
} from "./control-config";
import {
  FieldControlFactory,
  fieldControlRegistry,
  renderWidgetFromRegistry,
  widgetStyleVariables,
  type FieldControlBaseStyleProps,
  type WidgetStyleProps,
} from "./control-library";
import { PageFormProvider, usePageForm } from "./page-form-context.js";

type WorkflowPageRendererProps = {
  fieldBrandingStyleProps?: FieldControlBaseStyleProps;
  page: PageDefinition;
  runtimeContext: UiRuntimeContext;
  widgetBrandingStyleProps?: WidgetStyleProps;
};

type RecordValue = Readonly<Record<string, unknown>>;
type EditableTableRow = {
  field: string;
  value: string;
};
type SignaturePoint = {
  x: number;
  y: number;
};

const isRecord = (value: unknown): value is RecordValue =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const stringValue = (value: unknown, fallback = ""): string =>
  typeof value === "string" ? value : fallback;

const booleanValue = (value: unknown): boolean =>
  typeof value === "boolean" ? value : false;

const numberValue = (value: unknown, fallback: number): number =>
  typeof value === "number" ? value : fallback;

const recordsValue = (value: unknown): readonly RecordValue[] =>
  Array.isArray(value) ? value.filter(isRecord) : [];

const stringArrayValue = (value: unknown): readonly string[] =>
  Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];

const objectValue = (value: unknown): Record<string, unknown> =>
  isRecord(value) ? { ...value } : {};

const optionValue = (option: RecordValue): string =>
  stringValue(option.value, stringValue(option.label));

const optionLabel = (option: RecordValue): string =>
  stringValue(option.label, optionValue(option));

const optionGroup = (option: RecordValue): string => stringValue(option.group, "Other");

const optionMatches = (option: RecordValue, query: string): boolean => {
  const normalizedQuery = query.trim().toLowerCase();

  return (
    normalizedQuery.length === 0 ||
    optionLabel(option).toLowerCase().includes(normalizedQuery) ||
    optionValue(option).toLowerCase().includes(normalizedQuery) ||
    optionGroup(option).toLowerCase().includes(normalizedQuery)
  );
};

const hrDomainWidgetFamilies = new Set([
  "recruiting",
  "onboarding",
  "offboarding",
  "performance",
  "talent",
  "workforce",
  "scheduling",
  "leave",
  "benefits",
  "payroll",
  "employeeRelations",
  "compliance",
  "experience",
  "coreHris",
  "compAdvanced",
  "learning",
  "dei",
  "labor",
  "healthSafety",
  "serviceDelivery",
  "employeeFinance",
  "globalMobility",
  "privacy",
]);

const groupedOptions = (
  options: readonly RecordValue[],
): readonly { group: string; options: readonly RecordValue[] }[] => {
  const groups = new Map<string, RecordValue[]>();

  for (const option of options) {
    const group = optionGroup(option);
    groups.set(group, [...(groups.get(group) ?? []), option]);
  }

  return Array.from(groups.entries()).map(([group, groupOptions]) => ({
    group,
    options: groupOptions,
  }));
};

const uniqueRecordsByValue = (
  records: readonly RecordValue[],
): readonly RecordValue[] => {
  const seen = new Set<string>();
  const uniqueRecords: RecordValue[] = [];

  for (const record of records) {
    const value = optionValue(record);

    if (!seen.has(value)) {
      seen.add(value);
      uniqueRecords.push(record);
    }
  }

  return uniqueRecords;
};

const clusterGroupsValue = (
  groups: readonly RecordValue[],
  items: readonly RecordValue[],
): readonly RecordValue[] => {
  if (groups.length > 0) {
    return groups;
  }

  return uniqueRecordsByValue(
    items.map((item) => ({
      label: optionGroup(item),
      value: optionGroup(item),
    })),
  );
};

const createClusterState = (
  groups: readonly RecordValue[],
  items: readonly RecordValue[],
): Record<string, string[]> => {
  const resolvedGroups = clusterGroupsValue(groups, items);
  const firstGroup = resolvedGroups[0];
  const fallbackGroupId =
    firstGroup === undefined ? "Backlog" : optionValue(firstGroup);
  const clusters: Record<string, string[]> = {};

  for (const group of resolvedGroups) {
    clusters[optionValue(group)] = [];
  }

  for (const item of items) {
    const groupId = optionGroup(item);
    const targetGroup = clusters[groupId] === undefined ? fallbackGroupId : groupId;
    clusters[targetGroup] = [...(clusters[targetGroup] ?? []), optionValue(item)];
  }

  return clusters;
};

const fieldTypeIsHcmPicker = (type: string): boolean =>
  type === "permission_entity_picker" ||
  (type.endsWith("_picker") &&
    type !== "manager_tree_picker" &&
    type !== "org_tree_picker");

const valueToText = (value: unknown): string => {
  if (typeof value === "string") {
    return value;
  }

  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }

  if (value === undefined || value === null) {
    return "Not set";
  }

  return JSON.stringify(value);
};

const valueToInputText = (value: unknown): string =>
  typeof value === "string" || typeof value === "number" ? String(value) : "";

const constraintNumberValue = (value: unknown): number | undefined =>
  typeof value === "number" ? value : undefined;

const constraintScalarValue = (value: unknown): string | number | undefined =>
  typeof value === "string" || typeof value === "number" ? value : undefined;

const constraintTextValue = (value: unknown): string | undefined =>
  typeof value === "string" && value.length > 0 ? value : undefined;

const applyInputTransform = (field: RecordValue, value: string): string => {
  const transform = stringValue(field.transform, stringValue(field.format));

  if (transform === "uppercase") {
    return value.toUpperCase();
  }

  if (transform === "digits_only") {
    return value.replace(/\D/g, "");
  }

  if (transform === "alpha_numeric_upper") {
    return value.toUpperCase().replace(/[^A-Z0-9-]/g, "");
  }

  if (transform === "slug") {
    return value
      .toLowerCase()
      .replace(/[^a-z0-9-\s]/g, "")
      .trim()
      .replace(/\s+/g, "-");
  }

  if (transform === "phone_us") {
    const digits = value.replace(/\D/g, "").slice(0, 10);
    const area = digits.slice(0, 3);
    const prefix = digits.slice(3, 6);
    const line = digits.slice(6, 10);

    if (digits.length > 6) {
      return `(${area}) ${prefix}-${line}`;
    }

    if (digits.length > 3) {
      return `(${area}) ${prefix}`;
    }

    return area.length > 0 ? `(${area}` : "";
  }

  return value;
};

const inputConstraintAttributes = (
  field: RecordValue,
  required: boolean,
): InputHTMLAttributes<HTMLInputElement> => {
  const pattern = constraintTextValue(field.pattern);
  const validationMessage = constraintTextValue(field.validationMessage);
  const inputMode = constraintTextValue(field.inputMode);
  const autoComplete = constraintTextValue(field.autoComplete);
  const min = constraintScalarValue(field.min);
  const max = constraintScalarValue(field.max);
  const step = constraintNumberValue(field.step);
  const minLength = constraintNumberValue(field.minLength);
  const maxLength = constraintNumberValue(field.maxLength);

  return {
    ...(required ? { required: true } : {}),
    ...(booleanValue(field.readOnly) ? { readOnly: true } : {}),
    ...(booleanValue(field.disabled) ? { disabled: true } : {}),
    ...(pattern === undefined ? {} : { pattern }),
    ...(validationMessage === undefined ? {} : { title: validationMessage }),
    ...(inputMode === undefined
      ? {}
      : {
          inputMode: inputMode as InputHTMLAttributes<HTMLInputElement>["inputMode"],
        }),
    ...(autoComplete === undefined ? {} : { autoComplete }),
    ...(min === undefined ? {} : { min }),
    ...(max === undefined ? {} : { max }),
    ...(step === undefined ? {} : { step }),
    ...(minLength === undefined ? {} : { minLength }),
    ...(maxLength === undefined ? {} : { maxLength }),
  };
};

const textareaConstraintAttributes = (
  field: RecordValue,
  required: boolean,
): TextareaHTMLAttributes<HTMLTextAreaElement> => {
  const minLength = constraintNumberValue(field.minLength);
  const maxLength = constraintNumberValue(field.maxLength);

  return {
    ...(required ? { required: true } : {}),
    ...(booleanValue(field.readOnly) ? { readOnly: true } : {}),
    ...(booleanValue(field.disabled) ? { disabled: true } : {}),
    ...(minLength === undefined ? {} : { minLength }),
    ...(maxLength === undefined ? {} : { maxLength }),
  };
};

const constraintBadges = (field: RecordValue, required: boolean): readonly string[] => {
  const badges: string[] = [];

  if (required) {
    badges.push("required");
  }

  for (const key of [
    "min",
    "max",
    "step",
    "minLength",
    "maxLength",
    "pattern",
    "inputMode",
    "format",
    "transform",
  ]) {
    const value = field[key];

    if (value !== undefined) {
      badges.push(`${key}: ${String(value)}`);
    }
  }

  if (field.prefix !== undefined) {
    badges.push(`prefix: ${String(field.prefix)}`);
  }

  if (field.suffix !== undefined) {
    badges.push(`suffix: ${String(field.suffix)}`);
  }

  if (booleanValue(field.readOnly)) {
    badges.push("read only");
  }

  if (booleanValue(field.disabled)) {
    badges.push("disabled");
  }

  return badges;
};

const sanitizeHtmlSubset = (html: string): string =>
  html
    .replace(/<script[\s\S]*?<\/script>/gi, "")
    .replace(/\son[a-z]+="[^"]*"/gi, "")
    .replace(/\son[a-z]+='[^']*'/gi, "")
    .replace(/javascript:/gi, "");

const firstOptionValue = (options: readonly RecordValue[]): string =>
  options[0] === undefined ? "" : optionValue(options[0]);

const createInitialFieldValue = (field: RecordValue): unknown => {
  if (field.value !== undefined) {
    return field.value;
  }

  if (field.defaultValue !== undefined) {
    return field.defaultValue;
  }

  const type = stringValue(field.type, "text");
  const options = recordsValue(field.options);

  if (type === "checkbox" || type === "toggle" || type === "policy_acknowledgement") {
    return false;
  }

  if (type === "toggle_group") {
    return Object.fromEntries(
      options.map((option) => [optionValue(option), booleanValue(option.defaultValue)]),
    );
  }

  if (type === "slider") {
    return 50;
  }

  if (type === "slider_group" || type === "metric_slider_group") {
    return Object.fromEntries(
      options.map((option) => [
        optionValue(option),
        numberValue(option.defaultValue, 50),
      ]),
    );
  }

  if (type === "multi_select") {
    return options.slice(0, 2).map((option) => firstOptionValue([option]));
  }

  if (
    type === "radio" ||
    type === "radio_group" ||
    type === "select" ||
    type === "dropdown" ||
    type === "grouped_select" ||
    type === "dropdown_group" ||
    type === "filterable_select" ||
    type === "filterable_dropdown" ||
    type === "manager_tree_picker" ||
    fieldTypeIsHcmPicker(type)
  ) {
    return firstOptionValue(options);
  }

  if (type === "file" || type === "evidence_upload") {
    return [];
  }

  if (type === "repeating_list") {
    return ["Initial approver", "Backup approver"];
  }

  if (type === "table_editor") {
    return [
      { field: "Cost center", value: "CC-401 Clinical" },
      { field: "Pay band", value: "Nurse Band 4" },
    ];
  }

  if (type === "matrix") {
    const rows = recordsValue(field.rows);
    const columns = recordsValue(field.columns);
    const firstColumn = firstOptionValue(columns);
    const values: Record<string, string> = {};

    for (const row of rows) {
      values[optionValue(row)] = firstColumn;
    }

    return values;
  }

  if (type === "cluster_board" || type === "drag_drop_clusters") {
    const items =
      recordsValue(field.items).length > 0 ? recordsValue(field.items) : options;

    return createClusterState(recordsValue(field.groups), items);
  }

  if (type === "date_range") {
    return { start: "2026-06-01", end: "2026-06-15" };
  }

  if (type === "effective_dated_change") {
    return {
      effectiveDate: "2026-06-01",
      datingMode: "future",
      payrollCutoff: "2026-06-15",
      reason: "",
    };
  }

  if (type === "before_after_field_editor") {
    return {
      current: stringValue(field.current, "Current value"),
      proposed: stringValue(field.proposed, "Proposed value"),
      reason: "",
    };
  }

  if (type === "compensation_package_editor") {
    return {
      basePay: numberValue(field.basePay, 112000),
      currency: "USD",
      frequency: "annual",
      bonusTarget: numberValue(field.bonusTarget, 10),
      allowance: numberValue(field.allowance, 0),
    };
  }

  if (type === "manager_tree_picker") {
    return firstOptionValue(options);
  }

  if (type === "approval_chain_editor") {
    return recordsValue(field.items).map((item) => ({
      id: optionValue(item),
      approver: optionLabel(item),
      rule: stringValue(item.rule, "Required"),
      status: stringValue(item.status, "Pending"),
    }));
  }

  if (type === "policy_evidence_checklist") {
    return Object.fromEntries(
      recordsValue(field.items).map((item) => [
        optionValue(item),
        booleanValue(item.defaultValue),
      ]),
    );
  }

  if (type === "bulk_grid_editor") {
    return recordsValue(field.rows).length > 0
      ? recordsValue(field.rows)
      : [
          { employee: "Jane Rivera", field: "Department", proposed: "ICU" },
          { employee: "Mina Patel", field: "Manager", proposed: "Alex Manager" },
        ];
  }

  if (type === "conflict_resolver") {
    return Object.fromEntries(
      recordsValue(field.items).map((item) => [
        optionValue(item),
        stringValue(item.defaultResolution, "review"),
      ]),
    );
  }

  if (type === "integration_repair_control") {
    return Object.fromEntries(
      recordsValue(field.items).map((item) => [
        optionValue(item),
        stringValue(item.defaultAction, "retry"),
      ]),
    );
  }

  if (type === "ai_review_panel") {
    return { decision: "accept", note: "" };
  }

  if (type === "sensitive_field_reveal") {
    return { revealed: false, reason: "", acknowledged: false };
  }

  if (type === "international_contact") {
    return {
      country: "US",
      addressLine1: "100 Harbor Way",
      region: "MA",
      postalCode: "02139",
      phone: "+1 555 010 2040",
    };
  }

  if (type === "schedule_time_control") {
    return {
      timezone: "America/New_York",
      schedulePattern: "weekday_day_shift",
      weeklyHours: 40,
      fte: 1,
    };
  }

  if (type === "transaction_simulation_viewer") {
    return {
      approved: false,
      rollbackReviewed: false,
      releaseMode: "hold_for_approval",
    };
  }

  if (type === "signature" || type === "attestation" || type === "signature_capture") {
    return { method: "typed", typedName: "", attested: false, points: [] };
  }

  if (type.endsWith("_editor")) {
    return { current: "", proposed: "", notes: "" };
  }

  return stringValue(field.placeholder);
};

const slugifyFieldId = (label: string, index: number): string => {
  const slug = label
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");

  return slug.length > 0 ? `${slug}_${index}` : `field_${index}`;
};

/**
 * Guarantees every field in a dynamic field group has a non-empty, unique id.
 * The DOM relies on ids for `<label htmlFor>` targeting and the form-state map
 * keys controlled values by id, so duplicates collapse multiple fields onto a
 * single state slot. AI providers occasionally omit `id` (the schema only
 * requires `type`) — without this normalization those fields all fall back to
 * the literal "generated_control" string and overwrite each other.
 */
export const ensureUniqueFieldIds = (
  fields: readonly RecordValue[],
): readonly RecordValue[] => {
  const usedIds = new Set<string>();
  const normalized: RecordValue[] = [];

  fields.forEach((field, index) => {
    // AI generators often emit `name` instead of `id` (JSON-schema habit).
    // Accept either so the field renders with a stable, unique id.
    const declaredId = stringValue(field.id).trim();
    const declaredName = stringValue(field.name).trim();
    const label = stringValue(field.label);
    const candidate =
      declaredId.length > 0
        ? declaredId
        : declaredName.length > 0
          ? declaredName
          : slugifyFieldId(label, index);
    let uniqueId = candidate;
    let suffix = 1;

    while (usedIds.has(uniqueId)) {
      suffix += 1;
      uniqueId = `${candidate}_${suffix}`;
    }

    usedIds.add(uniqueId);
    normalized.push({ ...field, id: uniqueId });
  });

  return normalized;
};

const createInitialFormValues = (
  fields: readonly RecordValue[],
): Record<string, unknown> => {
  const values: Record<string, unknown> = {};

  for (const field of fields) {
    const id = stringValue(field.id);

    if (id.length > 0) {
      values[id] = createInitialFieldValue(field);
    }
  }

  return values;
};

const payloadKeyLabels: Readonly<Record<string, string>> = {
  current: "Current",
  proposed: "Proposed",
  reason: "Reason",
  effectiveDate: "Effective date",
  datingMode: "Timing",
  dateMode: "Timing",
  payrollCutoff: "Payroll cutoff",
};

const humanizePayloadKey = (key: string): string => {
  const configuredLabel = payloadKeyLabels[key];

  if (configuredLabel !== undefined) {
    return configuredLabel;
  }

  return key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
};

const payloadValueIsEmpty = (value: unknown): boolean =>
  value === undefined ||
  value === null ||
  (typeof value === "string" && value.trim().length === 0);

const formattedPayloadValue = (value: unknown): string => {
  if (typeof value === "boolean") {
    return value ? "Yes" : "No";
  }

  if (typeof value === "string" || typeof value === "number") {
    return String(value);
  }

  return valueToText(value);
};

const formattedPayloadEntryValue = (key: string, value: unknown): string => {
  if ((key === "datingMode" || key === "dateMode") && typeof value === "string") {
    if (value === "retroactive") {
      return "Retroactive";
    }

    if (value === "correction") {
      return "Correction";
    }

    if (value === "future") {
      return "Future dated";
    }
  }

  return formattedPayloadValue(value);
};

function PayloadValueSummary({ value }: { value: unknown }): JSX.Element {
  if (isRecord(value)) {
    const record = objectValue(value);
    const current = formattedPayloadValue(record.current);
    const proposed = formattedPayloadValue(record.proposed);

    if (!payloadValueIsEmpty(record.current) || !payloadValueIsEmpty(record.proposed)) {
      return (
        <span className="payload-change-summary">
          <span>{payloadValueIsEmpty(record.current) ? "Not set" : current}</span>
          <small>to</small>
          <strong>{payloadValueIsEmpty(record.proposed) ? "Not set" : proposed}</strong>
        </span>
      );
    }

    const entries = Object.entries(record).filter(([, entryValue]) => {
      if (Array.isArray(entryValue)) {
        return entryValue.length > 0;
      }

      return !payloadValueIsEmpty(entryValue);
    });

    if (entries.length === 0) {
      return <span className="payload-empty">No values set</span>;
    }

    return (
      <span className="payload-kv-list">
        {entries.slice(0, 4).map(([key, entryValue]) => (
          <span key={key}>
            <strong>{humanizePayloadKey(key)}</strong>
            <small>{formattedPayloadEntryValue(key, entryValue)}</small>
          </span>
        ))}
      </span>
    );
  }

  if (Array.isArray(value)) {
    return (
      <span>
        {value.length} {value.length === 1 ? "item" : "items"}
      </span>
    );
  }

  if (payloadValueIsEmpty(value)) {
    return <span className="payload-empty">No value set</span>;
  }

  return <span>{formattedPayloadValue(value)}</span>;
}

function StatusBadge({ status }: { status: string }): JSX.Element {
  const normalized = status.toLowerCase();
  const className =
    normalized === "success" || normalized === "low"
      ? "status-badge status-success"
      : normalized === "warning" || normalized === "medium"
        ? "status-badge status-warning"
        : normalized === "error" || normalized === "high"
          ? "status-badge status-error"
          : "status-badge status-info";

  return <span className={className}>{status}</span>;
}

function WidgetShell({
  brandingStyleProps,
  widget,
  children,
}: {
  brandingStyleProps?: WidgetStyleProps;
  widget: ResolvedWidget;
  children: JSX.Element;
}): JSX.Element {
  const size = widget.instance.size ?? widget.definition?.defaultSize ?? "full";
  const shellClassName =
    brandingStyleProps?.className === undefined
      ? `widget widget-size-${size}`
      : `widget widget-size-${size} ${brandingStyleProps.className}`;

  return (
    <section
      className={shellClassName}
      data-widget-canonical-type={widget.canonicalType}
      data-widget-id={widget.instance.id}
      data-widget-type={widget.instance.type}
      style={widgetStyleVariables(brandingStyleProps)}
    >
      <header className="widget-header">
        <div>
          <h2>{widget.instance.title ?? widget.definition?.displayName}</h2>
          {widget.instance.description !== undefined ? (
            <p>{widget.instance.description}</p>
          ) : null}
        </div>
      </header>
      {children}
    </section>
  );
}

function RequestQueue({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const requests = recordsValue(widget.instance.props?.requests);

  return (
    <div className="queue-list">
      {requests.map((request) => (
        <article className="queue-row" key={stringValue(request.id)}>
          <div>
            <strong>{stringValue(request.title, "Untitled request")}</strong>
            <span>{stringValue(request.employee, "No subject")}</span>
          </div>
          <div className="queue-row-meta">
            <StatusBadge status={stringValue(request.risk, "Info")} />
            <span>{stringValue(request.state)}</span>
            <small>{stringValue(request.due)}</small>
          </div>
        </article>
      ))}
    </div>
  );
}

function EmployeeSummary({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const employee = isRecord(widget.bindings.employee?.value)
    ? widget.bindings.employee.value
    : {};

  return (
    <div className="employee-summary">
      <div className="avatar" aria-hidden="true">
        {stringValue(employee.displayName, "Employee").slice(0, 2).toUpperCase()}
      </div>
      <div className="summary-grid">
        <div>
          <span>Name</span>
          <strong>{stringValue(employee.displayName, "Unknown employee")}</strong>
        </div>
        <div>
          <span>Role</span>
          <strong>{stringValue(employee.jobTitle, "Not set")}</strong>
        </div>
        <div>
          <span>Department</span>
          <strong>{stringValue(employee.department, "Not set")}</strong>
        </div>
        <div>
          <span>Manager</span>
          <strong>{stringValue(employee.manager, "Not set")}</strong>
        </div>
      </div>
    </div>
  );
}

function DynamicFieldGroup({
  brandingStyleProps,
  widget,
}: {
  brandingStyleProps?: FieldControlBaseStyleProps;
  widget: ResolvedWidget;
}): JSX.Element {
  const fields = ensureUniqueFieldIds(recordsValue(widget.instance.props?.fields));
  const [values, setValues] = useState<Record<string, unknown>>(() =>
    createInitialFormValues(fields),
  );
  const generatedPayload = JSON.stringify(values);
  const pageForm = usePageForm();

  // Sync the initial defaults into PageFormContext once so the action bar can
  // submit before the user touches any field. The empty-deps array is
  // intentional — we only want to seed on mount; subsequent edits flow through
  // `setFieldValue` below.
  useEffect(() => {
    for (const [fieldId, value] of Object.entries(values)) {
      pageForm.setFieldValue(fieldId, value);
    }
  }, []);

  const setFieldValue = (fieldId: string, value: unknown): void => {
    setValues((current) => ({
      ...current,
      [fieldId]: value,
    }));
    pageForm.setFieldValue(fieldId, value);
  };

  return (
    <form
      className="dynamic-field-group field-grid"
      aria-label={widget.instance.title}
      onSubmit={(event) => event.preventDefault()}
    >
      {fields.map((field) => {
        const config = createFieldControlConfig(field);
        const id = config.id;
        const label = config.label;
        const type = config.type;
        const required = config.required;
        const options = normalizeControlRecords(
          recordsValue(field.options),
          config.data,
        );
        const placeholder = config.placeholder;
        const help = config.help;
        const rows = normalizeControlRecords(recordsValue(field.rows), config.data);
        const columns = normalizeControlRecords(
          recordsValue(field.columns),
          config.data,
        );
        const groups = normalizeControlRecords(recordsValue(field.groups), config.data);
        const items = normalizeControlRecords(recordsValue(field.items), config.data);
        const style = {
          ...brandingStyleProps?.cssVariables,
          ...controlStyleVariables(config.style),
          ...brandingStyleProps?.style,
        };
        const badges =
          config.display.showConstraints === false
            ? []
            : constraintBadges(field, required);

        return (
          <div
            className={controlClassName(config, `field-control field-control-${type}`)}
            data-control-id={id}
            data-control-type={type}
            key={id}
            style={style}
          >
            <label htmlFor={id}>
              {label}
              {required ? <em>Required</em> : null}
            </label>
            <FieldInput
              config={config}
              {...(brandingStyleProps === undefined
                ? {}
                : { fieldBrandingStyleProps: brandingStyleProps })}
              id={id}
              label={label}
              columns={columns}
              field={field}
              groups={groups}
              items={items}
              onChange={(value) => setFieldValue(id, value)}
              options={options}
              placeholder={placeholder}
              required={required}
              rows={rows}
              type={type}
              value={values[id]}
            />
            {help.length > 0 ? <small>{help}</small> : null}
            {config.aiInstruction !== undefined && config.aiInstruction.length > 0 ? (
              <small className="ai-control-instruction">{config.aiInstruction}</small>
            ) : null}
            {badges.length > 0 ? (
              <div className="constraint-tags" aria-label={`${label} constraints`}>
                {badges.map((badge) => (
                  <span key={badge}>{badge}</span>
                ))}
              </div>
            ) : null}
          </div>
        );
      })}
      <section
        aria-label="Generated workflow data"
        className="form-preview"
        data-generated-payload={generatedPayload}
      >
        <header>
          <div>
            <strong>Workflow data</strong>
            <span>
              {fields.length} {fields.length === 1 ? "control" : "controls"}
            </span>
          </div>
        </header>
        <dl className="payload-summary-grid">
          {fields.map((field) => {
            const config = createFieldControlConfig(field);
            const id = config.id;

            return (
              <div
                className="payload-summary-card"
                data-control-id={id}
                data-control-type={config.type}
                key={id}
              >
                <dt>{config.label}</dt>
                <dd>
                  <PayloadValueSummary value={values[id]} />
                </dd>
              </div>
            );
          })}
        </dl>
        <input
          name="generatedPayload"
          readOnly
          type="hidden"
          value={generatedPayload}
        />
      </section>
    </form>
  );
}

function FieldInput({
  config,
  id,
  label,
  columns,
  field,
  fieldBrandingStyleProps,
  groups,
  items,
  onChange,
  options,
  placeholder,
  required,
  rows,
  type,
  value,
}: {
  config: FieldControlConfig;
  id: string;
  label: string;
  columns: readonly RecordValue[];
  field: RecordValue;
  fieldBrandingStyleProps?: FieldControlBaseStyleProps;
  groups: readonly RecordValue[];
  items: readonly RecordValue[];
  onChange: (value: unknown) => void;
  options: readonly RecordValue[];
  placeholder: string;
  required: boolean;
  rows: readonly RecordValue[];
  type: string;
  value: unknown;
}): JSX.Element {
  const sourceType = stringValue(config.raw.sourceType);
  const isReadonlyAlias = config.type === "readonly" && sourceType.length > 0;

  if (isReadonlyAlias && sourceType === "effective_dated_change") {
    return <EffectiveDatedChangeControl onChange={onChange} value={value} />;
  }

  if (isReadonlyAlias && sourceType === "before_after_field_editor") {
    return <BeforeAfterFieldEditorControl onChange={onChange} value={value} />;
  }

  if (isReadonlyAlias && sourceType === "compensation_package_editor") {
    return <CompensationPackageEditorControl onChange={onChange} value={value} />;
  }

  if (isReadonlyAlias && sourceType === "international_contact") {
    return <InternationalContactControl onChange={onChange} value={value} />;
  }

  if (isReadonlyAlias && sourceType === "schedule_time_control") {
    return <ScheduleTimeControl onChange={onChange} value={value} />;
  }

  if (isReadonlyAlias && sourceType === "approval_chain_editor") {
    return (
      <ApprovalChainEditorControl items={items} onChange={onChange} value={value} />
    );
  }

  if (isReadonlyAlias && sourceType === "policy_evidence_checklist") {
    return (
      <PolicyEvidenceChecklistControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (isReadonlyAlias && sourceType === "conflict_resolver") {
    return (
      <ConflictResolverControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (isReadonlyAlias && sourceType === "integration_repair_control") {
    return (
      <IntegrationRepairControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (isReadonlyAlias && sourceType === "ai_review_panel") {
    return (
      <AiReviewPanelControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (isReadonlyAlias && sourceType === "transaction_simulation_viewer") {
    return (
      <TransactionSimulationViewerControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (
    isReadonlyAlias &&
    (sourceType.endsWith("_editor") || sourceType === "attestation")
  ) {
    return (
      <CompositeControl
        id={id}
        label={label}
        onChange={onChange}
        type={sourceType}
        value={value}
      />
    );
  }

  if (fieldControlRegistry[config.type] !== undefined) {
    return (
      <FieldControlFactory
        columns={columns}
        config={config}
        cssVariables={controlStyleVariables(config.style)}
        {...(fieldBrandingStyleProps === undefined
          ? {}
          : { brandingStyleProps: fieldBrandingStyleProps })}
        groups={groups}
        items={items}
        onChange={onChange}
        options={options}
        rows={rows}
        value={value}
      />
    );
  }

  const optionNodes = options.map((option) => (
    <option key={optionValue(option)} value={optionValue(option)}>
      {optionLabel(option)}
    </option>
  ));
  const textValue = valueToInputText(value);
  const prefix = stringValue(field.prefix);
  const suffix = stringValue(field.suffix);

  if (type === "textarea") {
    return (
      <textarea
        {...textareaConstraintAttributes(field, required)}
        id={id}
        onChange={(event) => onChange(event.currentTarget.value)}
        placeholder={placeholder}
        value={textValue}
      />
    );
  }

  if (type === "filterable_select" || type === "filterable_dropdown") {
    return (
      <FilterableDropdownControl
        id={id}
        label={label}
        onChange={onChange}
        options={options}
        emptyLabel={config.data.emptyLabel}
        value={textValue}
      />
    );
  }

  if (fieldTypeIsHcmPicker(type)) {
    return (
      <EntityPickerControl
        id={id}
        label={label}
        onChange={onChange}
        options={options}
        type={type}
        emptyLabel={config.data.emptyLabel}
        value={textValue}
      />
    );
  }

  if (type === "grouped_select" || type === "dropdown_group") {
    return (
      <GroupedSelectControl
        id={id}
        label={label}
        onChange={onChange}
        options={options}
        value={textValue}
      />
    );
  }

  if (type === "select" || type === "dropdown") {
    return (
      <select
        id={id}
        onChange={(event) => onChange(event.currentTarget.value)}
        value={textValue}
      >
        <option value="" disabled>
          Select {label.toLowerCase()}
        </option>
        {optionNodes}
      </select>
    );
  }

  if (type === "multi_select") {
    return (
      <select
        id={id}
        multiple
        onChange={(event) =>
          onChange(
            Array.from(event.currentTarget.selectedOptions).map(
              (option) => option.value,
            ),
          )
        }
        value={stringArrayValue(value)}
      >
        {optionNodes}
      </select>
    );
  }

  if (type === "radio") {
    return (
      <fieldset className="choice-group">
        {options.map((option) => {
          const optionValue = stringValue(option.value, stringValue(option.label));

          return (
            <label key={optionValue}>
              <input
                checked={textValue === optionValue}
                name={id}
                onChange={(event) => onChange(event.currentTarget.value)}
                type="radio"
                value={optionValue}
              />
              {stringValue(option.label, optionValue)}
            </label>
          );
        })}
      </fieldset>
    );
  }

  if (type === "radio_group") {
    return (
      <GroupedRadioControl
        id={id}
        onChange={onChange}
        options={options}
        value={textValue}
      />
    );
  }

  if (type === "toggle_group") {
    return <ToggleGroupControl onChange={onChange} options={options} value={value} />;
  }

  if (type === "checkbox" || type === "policy_acknowledgement") {
    return (
      <label className="inline-choice">
        <input
          checked={booleanValue(value)}
          id={id}
          onChange={(event) => onChange(event.currentTarget.checked)}
          type="checkbox"
        />
        <span>{placeholder}</span>
      </label>
    );
  }

  if (type === "toggle") {
    return (
      <label className="toggle-control">
        <input
          checked={booleanValue(value)}
          id={id}
          onChange={(event) => onChange(event.currentTarget.checked)}
          type="checkbox"
        />
        <span aria-hidden="true" />
        <strong>{placeholder}</strong>
      </label>
    );
  }

  if (type === "slider") {
    const sliderValue = numberValue(value, 50);
    const min = numberValue(field.min, 0);
    const max = numberValue(field.max, 100);
    const step = numberValue(field.step, 1);
    const outputSuffix = stringValue(field.suffix, "%");

    return (
      <div className="slider-control">
        <input
          id={id}
          max={max}
          min={min}
          onChange={(event) => onChange(Number(event.currentTarget.value))}
          step={step}
          type="range"
          value={sliderValue}
        />
        <output htmlFor={id}>
          {sliderValue}
          {outputSuffix}
        </output>
      </div>
    );
  }

  if (type === "slider_group" || type === "metric_slider_group") {
    return <SliderGroupControl onChange={onChange} options={options} value={value} />;
  }

  if (type === "file" || type === "evidence_upload") {
    const files = stringArrayValue(value);

    return (
      <div className="file-control">
        <input
          id={id}
          onChange={(event) =>
            onChange(
              Array.from(event.currentTarget.files ?? []).map((file) => file.name),
            )
          }
          type="file"
        />
        <span>{files.length > 0 ? files.join(", ") : "No files selected"}</span>
      </div>
    );
  }

  if (type === "repeating_list") {
    return <RepeatingListControl onChange={onChange} value={value} />;
  }

  if (type === "table_editor") {
    return <TableEditorControl label={label} onChange={onChange} value={value} />;
  }

  if (type === "matrix") {
    return (
      <MatrixControl
        columns={columns}
        fieldLabel={label}
        onChange={onChange}
        rows={rows}
        value={value}
      />
    );
  }

  if (type === "cluster_board" || type === "drag_drop_clusters") {
    return (
      <ClusterBoardControl
        groups={groups}
        items={items.length > 0 ? items : options}
        label={label}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (type === "effective_dated_change") {
    return <EffectiveDatedChangeControl onChange={onChange} value={value} />;
  }

  if (type === "before_after_field_editor") {
    return <BeforeAfterFieldEditorControl onChange={onChange} value={value} />;
  }

  if (type === "compensation_package_editor") {
    return <CompensationPackageEditorControl onChange={onChange} value={value} />;
  }

  if (type === "manager_tree_picker" || type === "org_tree_picker") {
    return (
      <ManagerTreePickerControl
        label={label}
        onChange={onChange}
        options={options}
        value={textValue}
      />
    );
  }

  if (type === "approval_chain_editor") {
    return (
      <ApprovalChainEditorControl items={items} onChange={onChange} value={value} />
    );
  }

  if (type === "policy_evidence_checklist") {
    return (
      <PolicyEvidenceChecklistControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (type === "bulk_grid_editor") {
    return (
      <BulkGridEditorControl
        columns={columns}
        onChange={onChange}
        rows={rows}
        value={value}
      />
    );
  }

  if (type === "conflict_resolver") {
    return (
      <ConflictResolverControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (type === "integration_repair_control") {
    return (
      <IntegrationRepairControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (type === "ai_review_panel") {
    return (
      <AiReviewPanelControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (type === "sensitive_field_reveal") {
    return (
      <SensitiveFieldRevealControl
        label={label}
        onChange={onChange}
        placeholder={placeholder}
        value={value}
      />
    );
  }

  if (type === "international_contact") {
    return <InternationalContactControl onChange={onChange} value={value} />;
  }

  if (type === "schedule_time_control") {
    return <ScheduleTimeControl onChange={onChange} value={value} />;
  }

  if (type === "transaction_simulation_viewer") {
    return (
      <TransactionSimulationViewerControl
        items={items.length > 0 ? items : options}
        onChange={onChange}
        value={value}
      />
    );
  }

  if (
    type.endsWith("_editor") ||
    type === "date_range" ||
    type === "signature" ||
    type === "attestation"
  ) {
    return (
      <CompositeControl
        id={id}
        label={label}
        onChange={onChange}
        type={type}
        value={value}
      />
    );
  }

  if (type === "signature_capture") {
    return <SignatureCaptureControl id={id} onChange={onChange} value={value} />;
  }

  const htmlType =
    type === "number" ||
    type === "money" ||
    type === "percent" ||
    type === "date" ||
    type === "time" ||
    type === "email" ||
    type === "url"
      ? type
      : type === "phone"
        ? "tel"
        : "text";

  const inputNode = (
    <input
      {...inputConstraintAttributes(field, required)}
      id={id}
      onChange={(event) =>
        onChange(applyInputTransform(field, event.currentTarget.value))
      }
      placeholder={placeholder}
      type={htmlType}
      value={textValue}
    />
  );

  if (prefix.length > 0 || suffix.length > 0) {
    return (
      <div className="formatted-input">
        {prefix.length > 0 ? <span>{prefix}</span> : null}
        {inputNode}
        {suffix.length > 0 ? <span>{suffix}</span> : null}
      </div>
    );
  }

  return inputNode;
}

function FilterableDropdownControl({
  emptyLabel,
  id,
  label,
  onChange,
  options,
  value,
}: {
  emptyLabel?: string | undefined;
  id: string;
  label: string;
  onChange: (value: unknown) => void;
  options: readonly RecordValue[];
  value: string;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const filteredOptions = options.filter((option) => optionMatches(option, query));
  const selected = options.find((option) => optionValue(option) === value);

  return (
    <div className="filterable-dropdown">
      <input
        aria-controls={`${id}-options`}
        aria-label={`Filter ${label}`}
        onChange={(event) => setQuery(event.currentTarget.value)}
        placeholder={`Filter ${label.toLowerCase()}`}
        role="combobox"
        value={query}
      />
      <div className="selected-pill">
        Selected:{" "}
        {selected === undefined ? (emptyLabel ?? "None") : optionLabel(selected)}
      </div>
      <div className="filterable-dropdown-list" id={`${id}-options`} role="listbox">
        {filteredOptions.map((option) => {
          const currentValue = optionValue(option);
          const selectedOption = currentValue === value;

          return (
            <button
              aria-selected={selectedOption}
              className={
                selectedOption ? "option-row option-row-selected" : "option-row"
              }
              key={currentValue}
              onClick={() => {
                onChange(currentValue);
                setQuery("");
              }}
              role="option"
              type="button"
            >
              <span>{optionLabel(option)}</span>
              <small>{optionGroup(option)}</small>
            </button>
          );
        })}
        {filteredOptions.length === 0 ? (
          <p className="placeholder-text">No matching options</p>
        ) : null}
      </div>
    </div>
  );
}

function EntityPickerControl({
  emptyLabel,
  id,
  label,
  onChange,
  options,
  type,
  value,
}: {
  emptyLabel?: string | undefined;
  id: string;
  label: string;
  onChange: (value: unknown) => void;
  options: readonly RecordValue[];
  type: string;
  value: string;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const filteredOptions = options.filter((option) => optionMatches(option, query));
  const selectedOption = options.find((option) => optionValue(option) === value);
  const pickerScope = type.replace(/_/g, " ").replace("picker", "").trim();

  return (
    <div className="entity-picker-control">
      <input
        aria-controls={`${id}-entity-options`}
        aria-label={`Search ${label}`}
        onChange={(event) => setQuery(event.currentTarget.value)}
        placeholder={`Search ${label.toLowerCase()}`}
        value={query}
      />
      <div className="selected-pill">
        {selectedOption === undefined
          ? (emptyLabel ?? `No ${pickerScope || "entity"} selected`)
          : `${optionLabel(selectedOption)} · ${stringValue(
              selectedOption.scope,
              optionGroup(selectedOption),
            )}`}
      </div>
      <div className="entity-picker-list" id={`${id}-entity-options`} role="listbox">
        {filteredOptions.map((option) => {
          const currentValue = optionValue(option);
          const selected = currentValue === value;
          const status = stringValue(option.status, "Allowed");
          const permission = stringValue(option.permission, "RBAC scoped");

          return (
            <button
              aria-selected={selected}
              className={
                selected ? "entity-option entity-option-selected" : "entity-option"
              }
              key={currentValue}
              onClick={() => {
                onChange(currentValue);
                setQuery("");
              }}
              role="option"
              type="button"
            >
              <span>
                <strong>{optionLabel(option)}</strong>
                <small>{stringValue(option.description, optionGroup(option))}</small>
              </span>
              <span className="entity-option-meta">
                <StatusBadge status={status} />
                <small>{permission}</small>
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function GroupedSelectControl({
  id,
  label,
  onChange,
  options,
  value,
}: {
  id: string;
  label: string;
  onChange: (value: unknown) => void;
  options: readonly RecordValue[];
  value: string;
}): JSX.Element {
  return (
    <select
      id={id}
      onChange={(event) => onChange(event.currentTarget.value)}
      value={value}
    >
      <option value="" disabled>
        Select {label.toLowerCase()}
      </option>
      {groupedOptions(options).map((group) => (
        <optgroup key={group.group} label={group.group}>
          {group.options.map((option) => (
            <option key={optionValue(option)} value={optionValue(option)}>
              {optionLabel(option)}
            </option>
          ))}
        </optgroup>
      ))}
    </select>
  );
}

function GroupedRadioControl({
  id,
  onChange,
  options,
  value,
}: {
  id: string;
  onChange: (value: unknown) => void;
  options: readonly RecordValue[];
  value: string;
}): JSX.Element {
  return (
    <div className="grouped-radio-control">
      {groupedOptions(options).map((group) => (
        <fieldset className="choice-group" key={group.group}>
          <legend>{group.group}</legend>
          {group.options.map((option) => {
            const currentValue = optionValue(option);

            return (
              <label key={currentValue}>
                <input
                  checked={value === currentValue}
                  name={id}
                  onChange={(event) => onChange(event.currentTarget.value)}
                  type="radio"
                  value={currentValue}
                />
                {optionLabel(option)}
              </label>
            );
          })}
        </fieldset>
      ))}
    </div>
  );
}

function ToggleGroupControl({
  onChange,
  options,
  value,
}: {
  onChange: (value: unknown) => void;
  options: readonly RecordValue[];
  value: unknown;
}): JSX.Element {
  const toggles = objectValue(value);

  const setToggleValue = (key: string, enabled: boolean): void => {
    onChange({
      ...toggles,
      [key]: enabled,
    });
  };

  return (
    <div className="toggle-group-control">
      {options.map((option) => {
        const key = optionValue(option);
        const checked = booleanValue(toggles[key]);

        return (
          <label className="toggle-card" key={key}>
            <span>
              <strong>{optionLabel(option)}</strong>
              <small>{stringValue(option.description, optionGroup(option))}</small>
            </span>
            <input
              checked={checked}
              onChange={(event) => setToggleValue(key, event.currentTarget.checked)}
              type="checkbox"
            />
            <i aria-hidden="true" />
          </label>
        );
      })}
    </div>
  );
}

function SliderGroupControl({
  onChange,
  options,
  value,
}: {
  onChange: (value: unknown) => void;
  options: readonly RecordValue[];
  value: unknown;
}): JSX.Element {
  const sliderValues = objectValue(value);

  const setSliderValue = (key: string, nextValue: number): void => {
    onChange({
      ...sliderValues,
      [key]: nextValue,
    });
  };

  return (
    <div className="slider-group-control">
      {options.map((option) => {
        const key = optionValue(option);
        const min = numberValue(option.min, 0);
        const max = numberValue(option.max, 100);
        const step = numberValue(option.step, 1);
        const currentValue = numberValue(
          sliderValues[key],
          numberValue(option.defaultValue, 50),
        );

        return (
          <div className="slider-row" key={key}>
            <div className="slider-row-header">
              <span>
                <strong>{optionLabel(option)}</strong>
                <small>{stringValue(option.description, optionGroup(option))}</small>
              </span>
              <output>{currentValue}</output>
            </div>
            <input
              aria-label={optionLabel(option)}
              max={max}
              min={min}
              onChange={(event) =>
                setSliderValue(key, Number(event.currentTarget.value))
              }
              step={step}
              type="range"
              value={currentValue}
            />
            <div className="slider-track-meta">
              <span>{min}</span>
              <span>{max}</span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function ClusterBoardControl({
  groups,
  items,
  label,
  onChange,
  value,
}: {
  groups: readonly RecordValue[];
  items: readonly RecordValue[];
  label: string;
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const resolvedGroups = clusterGroupsValue(groups, items);
  const currentClusters = objectValue(value);
  const [draggedItem, setDraggedItem] = useState("");
  const itemLookup = new Map(items.map((item) => [optionValue(item), item]));
  const fallbackClusters = createClusterState(resolvedGroups, items);
  const clusters = Object.fromEntries(
    resolvedGroups.map((group) => {
      const groupId = optionValue(group);
      const values =
        currentClusters[groupId] === undefined
          ? (fallbackClusters[groupId] ?? [])
          : stringArrayValue(currentClusters[groupId]);

      return [groupId, values];
    }),
  );

  const moveItem = (itemId: string, targetGroupId: string): void => {
    if (itemId.length === 0) {
      return;
    }

    const nextClusters = Object.fromEntries(
      resolvedGroups.map((group) => {
        const groupId = optionValue(group);
        const groupItems = stringArrayValue(clusters[groupId]).filter(
          (currentItem) => currentItem !== itemId,
        );

        return [
          groupId,
          groupId === targetGroupId ? [...groupItems, itemId] : groupItems,
        ];
      }),
    );

    onChange(nextClusters);
    setDraggedItem("");
  };

  const readDraggedItem = (event: DragEvent<HTMLElement>): string =>
    event.dataTransfer.getData("text/plain") || draggedItem;

  return (
    <div className="cluster-board-control" aria-label={label}>
      {resolvedGroups.map((group) => {
        const groupId = optionValue(group);
        const groupItems = stringArrayValue(clusters[groupId]);

        return (
          <section
            className="cluster-column"
            key={groupId}
            onDragOver={(event) => event.preventDefault()}
            onDrop={(event) => {
              event.preventDefault();
              moveItem(readDraggedItem(event), groupId);
            }}
          >
            <header>
              <strong>{optionLabel(group)}</strong>
              <span>{groupItems.length}</span>
            </header>
            <div className="cluster-drop-zone">
              {groupItems.map((itemId) => {
                const item = itemLookup.get(itemId);

                return (
                  <article
                    className={
                      draggedItem === itemId
                        ? "cluster-card cluster-card-dragging"
                        : "cluster-card"
                    }
                    draggable
                    key={itemId}
                    onDragEnd={() => setDraggedItem("")}
                    onDragStart={(event) => {
                      setDraggedItem(itemId);
                      event.dataTransfer.effectAllowed = "move";
                      event.dataTransfer.setData("text/plain", itemId);
                    }}
                  >
                    <strong>{item === undefined ? itemId : optionLabel(item)}</strong>
                    <small>
                      {item === undefined
                        ? "Workflow item"
                        : stringValue(item.description, optionGroup(item))}
                    </small>
                  </article>
                );
              })}
            </div>
          </section>
        );
      })}
    </div>
  );
}

function EffectiveDatedChangeControl({
  onChange,
  value,
}: {
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const change = objectValue(value);
  const datingMode = stringValue(change.datingMode, "future");
  const datingModeLabel =
    datingMode === "retroactive"
      ? "Retroactive"
      : datingMode === "correction"
        ? "Correction"
        : "Future dated";
  const payrollCutoff = stringValue(change.payrollCutoff, "not set");

  const updateChange = (nextValue: Record<string, unknown>): void => {
    onChange({ ...change, ...nextValue });
  };

  return (
    <div className="hcm-control-card">
      <div className="segmented-control" role="group" aria-label="Dating mode">
        {[
          { label: "Future", value: "future" },
          { label: "Retro", value: "retroactive" },
          { label: "Correction", value: "correction" },
        ].map((mode) => (
          <button
            aria-pressed={datingMode === mode.value}
            className={datingMode === mode.value ? "segment-active" : ""}
            key={mode.value}
            onClick={() => updateChange({ datingMode: mode.value })}
            type="button"
          >
            {mode.label}
          </button>
        ))}
      </div>
      <div className="metadata-grid">
        <label>
          <span>Effective date</span>
          <input
            onChange={(event) =>
              updateChange({ effectiveDate: event.currentTarget.value })
            }
            type="date"
            value={stringValue(change.effectiveDate)}
          />
        </label>
        <label>
          <span>Payroll cutoff</span>
          <input
            onChange={(event) =>
              updateChange({ payrollCutoff: event.currentTarget.value })
            }
            type="date"
            value={stringValue(change.payrollCutoff)}
          />
        </label>
      </div>
      <label className="hcm-note-field">
        <span>Change reason</span>
        <textarea
          onChange={(event) => updateChange({ reason: event.currentTarget.value })}
          placeholder="Reason for effective dating"
          value={stringValue(change.reason)}
        />
      </label>
      <div className="selected-pill">
        <span>{datingModeLabel}</span>
        <span>Cutoff {payrollCutoff}</span>
      </div>
    </div>
  );
}

function BeforeAfterFieldEditorControl({
  onChange,
  value,
}: {
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const diff = objectValue(value);

  const updateDiff = (nextValue: Record<string, unknown>): void => {
    onChange({ ...diff, ...nextValue });
  };

  return (
    <div className="hcm-control-card before-after-control-card">
      <div className="metadata-grid">
        <label>
          <span>Current value</span>
          <input readOnly value={stringValue(diff.current)} />
        </label>
        <label>
          <span>Proposed value</span>
          <input
            onChange={(event) => updateDiff({ proposed: event.currentTarget.value })}
            value={stringValue(diff.proposed)}
          />
        </label>
      </div>
      <label className="hcm-note-field">
        <span>Change reason</span>
        <textarea
          onChange={(event) => updateDiff({ reason: event.currentTarget.value })}
          placeholder="Reason or reviewer note"
          value={stringValue(diff.reason)}
        />
      </label>
    </div>
  );
}

function CompensationPackageEditorControl({
  onChange,
  value,
}: {
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const compensation = objectValue(value);

  const updateCompensation = (nextValue: Record<string, unknown>): void => {
    onChange({ ...compensation, ...nextValue });
  };

  return (
    <div className="hcm-control-card">
      <div className="metadata-grid">
        <label>
          <span>Base pay</span>
          <input
            min="0"
            onChange={(event) =>
              updateCompensation({ basePay: Number(event.currentTarget.value) })
            }
            type="number"
            value={numberValue(compensation.basePay, 0)}
          />
        </label>
        <label>
          <span>Currency</span>
          <select
            onChange={(event) =>
              updateCompensation({ currency: event.currentTarget.value })
            }
            value={stringValue(compensation.currency, "USD")}
          >
            <option value="USD">USD</option>
            <option value="EUR">EUR</option>
            <option value="GBP">GBP</option>
            <option value="CAD">CAD</option>
          </select>
        </label>
        <label>
          <span>Frequency</span>
          <select
            onChange={(event) =>
              updateCompensation({ frequency: event.currentTarget.value })
            }
            value={stringValue(compensation.frequency, "annual")}
          >
            <option value="annual">Annual</option>
            <option value="hourly">Hourly</option>
            <option value="monthly">Monthly</option>
            <option value="per_pay_period">Per pay period</option>
          </select>
        </label>
        <label>
          <span>Bonus target %</span>
          <input
            min="0"
            onChange={(event) =>
              updateCompensation({ bonusTarget: Number(event.currentTarget.value) })
            }
            type="number"
            value={numberValue(compensation.bonusTarget, 0)}
          />
        </label>
        <label>
          <span>Allowance</span>
          <input
            min="0"
            onChange={(event) =>
              updateCompensation({ allowance: Number(event.currentTarget.value) })
            }
            type="number"
            value={numberValue(compensation.allowance, 0)}
          />
        </label>
      </div>
      <div className="selected-pill">
        {valueToText(compensation.currency)} {valueToText(compensation.basePay)} ·{" "}
        {valueToText(compensation.frequency)}
      </div>
    </div>
  );
}

function ManagerTreePickerControl({
  label,
  onChange,
  options,
  value,
}: {
  label: string;
  onChange: (value: unknown) => void;
  options: readonly RecordValue[];
  value: string;
}): JSX.Element {
  const levels = Array.from(
    new Set(options.map((option) => numberValue(option.level, 0))),
  ).sort((left, right) => left - right);

  return (
    <div
      className="tree-picker-control org-picker-control"
      role="tree"
      aria-label={label}
    >
      {levels.map((level) => (
        <div className="org-picker-level" key={level}>
          {options
            .filter((option) => numberValue(option.level, 0) === level)
            .map((option) => {
              const currentValue = optionValue(option);

              return (
                <button
                  aria-selected={value === currentValue}
                  className={
                    value === currentValue
                      ? "tree-node tree-node-selected"
                      : "tree-node"
                  }
                  key={currentValue}
                  onClick={() => onChange(currentValue)}
                  type="button"
                >
                  <span>
                    <strong>{optionLabel(option)}</strong>
                    <small>
                      {stringValue(option.description, optionGroup(option))}
                    </small>
                  </span>
                  <StatusBadge status={stringValue(option.status, "Allowed")} />
                </button>
              );
            })}
        </div>
      ))}
    </div>
  );
}

function ApprovalChainEditorControl({
  items,
  onChange,
  value,
}: {
  items: readonly RecordValue[];
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const approvals = recordsValue(value);
  const approvalRows = approvals.length > 0 ? approvals : items;

  const updateApproval = (rowIndex: number, key: string, nextValue: string): void => {
    onChange(
      approvalRows.map((approval, index) =>
        index === rowIndex ? { ...approval, [key]: nextValue } : approval,
      ),
    );
  };

  return (
    <div className="hcm-control-list">
      {approvalRows.map((approval, index) => (
        <article
          className="workflow-control-row"
          key={`${optionValue(approval)}-${index}`}
        >
          <span>
            <strong>{stringValue(approval.approver, optionLabel(approval))}</strong>
            <small>{stringValue(approval.rule, "Required")}</small>
          </span>
          <select
            aria-label={`Approval status ${index + 1}`}
            onChange={(event) =>
              updateApproval(index, "status", event.currentTarget.value)
            }
            value={stringValue(approval.status, "Pending")}
          >
            <option value="Pending">Pending</option>
            <option value="Approved">Approved</option>
            <option value="Escalated">Escalated</option>
            <option value="Skipped">Skipped</option>
          </select>
        </article>
      ))}
    </div>
  );
}

function PolicyEvidenceChecklistControl({
  items,
  onChange,
  value,
}: {
  items: readonly RecordValue[];
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const checklist = objectValue(value);

  const updateChecklist = (itemId: string, checked: boolean): void => {
    onChange({ ...checklist, [itemId]: checked });
  };

  return (
    <div className="hcm-control-list">
      {items.map((item) => {
        const itemId = optionValue(item);
        const checked = booleanValue(checklist[itemId]);

        return (
          <label className="workflow-check-row" key={itemId}>
            <input
              checked={checked}
              onChange={(event) => updateChecklist(itemId, event.currentTarget.checked)}
              type="checkbox"
            />
            <span>
              <strong>{optionLabel(item)}</strong>
              <small>{stringValue(item.description, optionGroup(item))}</small>
            </span>
            <StatusBadge
              status={checked ? "success" : stringValue(item.status, "warning")}
            />
          </label>
        );
      })}
    </div>
  );
}

function BulkGridEditorControl({
  columns,
  onChange,
  rows,
  value,
}: {
  columns: readonly RecordValue[];
  onChange: (value: unknown) => void;
  rows: readonly RecordValue[];
  value: unknown;
}): JSX.Element {
  const gridColumns =
    columns.length > 0
      ? columns
      : [
          { label: "Employee", value: "employee" },
          { label: "Field", value: "field" },
          { label: "Proposed", value: "proposed" },
        ];
  const gridRows = recordsValue(value).length > 0 ? recordsValue(value) : rows;

  const updateCell = (rowIndex: number, columnId: string, nextValue: string): void => {
    onChange(
      gridRows.map((row, index) =>
        index === rowIndex ? { ...row, [columnId]: nextValue } : row,
      ),
    );
  };

  return (
    <div className="bulk-grid-control">
      <div
        className="bulk-grid"
        style={{
          gridTemplateColumns: `repeat(${gridColumns.length}, minmax(140px, 1fr))`,
        }}
      >
        {gridColumns.map((column) => (
          <strong key={optionValue(column)}>{optionLabel(column)}</strong>
        ))}
        {gridRows.map((row, rowIndex) =>
          gridColumns.map((column) => {
            const columnId = optionValue(column);

            return (
              <input
                aria-label={`Row ${rowIndex + 1} ${optionLabel(column)}`}
                key={`${rowIndex}-${columnId}`}
                onChange={(event) =>
                  updateCell(rowIndex, columnId, event.currentTarget.value)
                }
                value={stringValue(row[columnId])}
              />
            );
          }),
        )}
      </div>
      <button
        className="secondary-inline-button"
        onClick={() => onChange([...gridRows, {}])}
        type="button"
      >
        Add row
      </button>
    </div>
  );
}

function ConflictResolverControl({
  items,
  onChange,
  value,
}: {
  items: readonly RecordValue[];
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const resolutions = objectValue(value);

  const updateResolution = (itemId: string, resolution: string): void => {
    onChange({ ...resolutions, [itemId]: resolution });
  };

  return (
    <div className="hcm-control-list">
      {items.map((item) => {
        const itemId = optionValue(item);

        return (
          <article className="workflow-control-row" key={itemId}>
            <span>
              <strong>{optionLabel(item)}</strong>
              <small>
                {stringValue(item.description, "Conflicting workflow fact")}
              </small>
            </span>
            <select
              aria-label={`Resolve ${optionLabel(item)}`}
              onChange={(event) => updateResolution(itemId, event.currentTarget.value)}
              value={stringValue(resolutions[itemId], "review")}
            >
              <option value="review">Manual review</option>
              <option value="use_current">Use current</option>
              <option value="use_proposed">Use proposed</option>
              <option value="merge">Merge</option>
            </select>
          </article>
        );
      })}
    </div>
  );
}

function IntegrationRepairControl({
  items,
  onChange,
  value,
}: {
  items: readonly RecordValue[];
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const actions = objectValue(value);

  const updateAction = (itemId: string, action: string): void => {
    onChange({ ...actions, [itemId]: action });
  };

  return (
    <div className="hcm-control-list">
      {items.map((item) => {
        const itemId = optionValue(item);

        return (
          <article className="workflow-control-row" key={itemId}>
            <span>
              <strong>{optionLabel(item)}</strong>
              <small>{stringValue(item.description, "External system write")}</small>
            </span>
            <StatusBadge status={stringValue(item.status, "warning")} />
            <select
              aria-label={`Repair action ${optionLabel(item)}`}
              onChange={(event) => updateAction(itemId, event.currentTarget.value)}
              value={stringValue(actions[itemId], "retry")}
            >
              <option value="retry">Retry</option>
              <option value="rollback">Rollback</option>
              <option value="manual_repair">Manual repair</option>
              <option value="suppress">Suppress</option>
            </select>
          </article>
        );
      })}
    </div>
  );
}

function AiReviewPanelControl({
  items,
  onChange,
  value,
}: {
  items: readonly RecordValue[];
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const review = objectValue(value);

  const updateReview = (nextValue: Record<string, unknown>): void => {
    onChange({ ...review, ...nextValue });
  };

  return (
    <div className="hcm-control-card">
      <div className="hcm-control-list">
        {items.map((item) => (
          <article className="workflow-control-row" key={optionValue(item)}>
            <span>
              <strong>{optionLabel(item)}</strong>
              <small>{stringValue(item.description, "AI review signal")}</small>
            </span>
            <StatusBadge status={stringValue(item.status, "info")} />
          </article>
        ))}
      </div>
      <div className="segmented-control" role="group" aria-label="AI review decision">
        {[
          { label: "Accept", value: "accept" },
          { label: "Dismiss", value: "dismiss" },
          { label: "Escalate", value: "escalate" },
        ].map((decision) => (
          <button
            aria-pressed={stringValue(review.decision) === decision.value}
            className={
              stringValue(review.decision) === decision.value ? "segment-active" : ""
            }
            key={decision.value}
            onClick={() => updateReview({ decision: decision.value })}
            type="button"
          >
            {decision.label}
          </button>
        ))}
      </div>
      <textarea
        onChange={(event) => updateReview({ note: event.currentTarget.value })}
        placeholder="Reviewer note"
        value={stringValue(review.note)}
      />
    </div>
  );
}

function SensitiveFieldRevealControl({
  label,
  onChange,
  placeholder,
  value,
}: {
  label: string;
  onChange: (value: unknown) => void;
  placeholder: string;
  value: unknown;
}): JSX.Element {
  const reveal = objectValue(value);
  const revealed = booleanValue(reveal.revealed);

  const updateReveal = (nextValue: Record<string, unknown>): void => {
    onChange({ ...reveal, ...nextValue });
  };

  return (
    <div className="hcm-control-card">
      <div className="sensitive-value">
        <span>{label}</span>
        <strong>{revealed ? placeholder : "Restricted value hidden"}</strong>
      </div>
      <textarea
        onChange={(event) => updateReveal({ reason: event.currentTarget.value })}
        placeholder="Reason for access"
        value={stringValue(reveal.reason)}
      />
      <label className="inline-choice">
        <input
          checked={booleanValue(reveal.acknowledged)}
          onChange={(event) =>
            updateReveal({ acknowledged: event.currentTarget.checked })
          }
          type="checkbox"
        />
        <span>Record this reveal in the audit ledger.</span>
      </label>
      <button
        className="secondary-inline-button"
        disabled={!booleanValue(reveal.acknowledged)}
        onClick={() => updateReveal({ revealed: !revealed })}
        type="button"
      >
        {revealed ? "Hide value" : "Reveal value"}
      </button>
    </div>
  );
}

function InternationalContactControl({
  onChange,
  value,
}: {
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const contact = objectValue(value);

  const updateContact = (nextValue: Record<string, unknown>): void => {
    onChange({ ...contact, ...nextValue });
  };

  return (
    <div className="hcm-control-card">
      <div className="metadata-grid">
        <label>
          <span>Country</span>
          <select
            onChange={(event) => updateContact({ country: event.currentTarget.value })}
            value={stringValue(contact.country, "US")}
          >
            <option value="US">United States</option>
            <option value="CA">Canada</option>
            <option value="GB">United Kingdom</option>
            <option value="DE">Germany</option>
          </select>
        </label>
        <label>
          <span>Phone</span>
          <input
            onChange={(event) => updateContact({ phone: event.currentTarget.value })}
            type="tel"
            value={stringValue(contact.phone)}
          />
        </label>
        <label>
          <span>Address line 1</span>
          <input
            onChange={(event) =>
              updateContact({ addressLine1: event.currentTarget.value })
            }
            value={stringValue(contact.addressLine1)}
          />
        </label>
        <label>
          <span>Region</span>
          <input
            onChange={(event) => updateContact({ region: event.currentTarget.value })}
            value={stringValue(contact.region)}
          />
        </label>
        <label>
          <span>Postal code</span>
          <input
            onChange={(event) =>
              updateContact({ postalCode: event.currentTarget.value })
            }
            value={stringValue(contact.postalCode)}
          />
        </label>
      </div>
    </div>
  );
}

function ScheduleTimeControl({
  onChange,
  value,
}: {
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const schedule = objectValue(value);

  const updateSchedule = (nextValue: Record<string, unknown>): void => {
    onChange({ ...schedule, ...nextValue });
  };

  return (
    <div className="hcm-control-card">
      <div className="metadata-grid">
        <label>
          <span>Time zone</span>
          <select
            onChange={(event) =>
              updateSchedule({ timezone: event.currentTarget.value })
            }
            value={stringValue(schedule.timezone, "America/New_York")}
          >
            <option value="America/New_York">America/New_York</option>
            <option value="America/Chicago">America/Chicago</option>
            <option value="America/Los_Angeles">America/Los_Angeles</option>
            <option value="Europe/London">Europe/London</option>
          </select>
        </label>
        <label>
          <span>Schedule pattern</span>
          <select
            onChange={(event) =>
              updateSchedule({ schedulePattern: event.currentTarget.value })
            }
            value={stringValue(schedule.schedulePattern, "weekday_day_shift")}
          >
            <option value="weekday_day_shift">Weekday day shift</option>
            <option value="rotating_shift">Rotating shift</option>
            <option value="compressed_week">Compressed week</option>
            <option value="flex">Flexible</option>
          </select>
        </label>
        <label>
          <span>Weekly hours</span>
          <input
            min="0"
            onChange={(event) =>
              updateSchedule({ weeklyHours: Number(event.currentTarget.value) })
            }
            type="number"
            value={numberValue(schedule.weeklyHours, 40)}
          />
        </label>
        <label>
          <span>FTE</span>
          <input
            max="1"
            min="0"
            onChange={(event) =>
              updateSchedule({ fte: Number(event.currentTarget.value) })
            }
            step="0.05"
            type="number"
            value={numberValue(schedule.fte, 1)}
          />
        </label>
      </div>
    </div>
  );
}

function TransactionSimulationViewerControl({
  items,
  onChange,
  value,
}: {
  items: readonly RecordValue[];
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const simulation = objectValue(value);

  const updateSimulation = (nextValue: Record<string, unknown>): void => {
    onChange({ ...simulation, ...nextValue });
  };

  return (
    <div className="hcm-control-card">
      <div className="hcm-control-list">
        {items.map((item, index) => (
          <article
            className="workflow-control-row"
            key={`${optionValue(item)}-${index}`}
          >
            <span>
              <strong>{optionLabel(item)}</strong>
              <small>{stringValue(item.description, "Planned transaction step")}</small>
            </span>
            <StatusBadge status={stringValue(item.status, "info")} />
          </article>
        ))}
      </div>
      <label className="inline-choice">
        <input
          checked={booleanValue(simulation.rollbackReviewed)}
          onChange={(event) =>
            updateSimulation({ rollbackReviewed: event.currentTarget.checked })
          }
          type="checkbox"
        />
        <span>Rollback path reviewed.</span>
      </label>
      <label className="inline-choice">
        <input
          checked={booleanValue(simulation.approved)}
          onChange={(event) =>
            updateSimulation({ approved: event.currentTarget.checked })
          }
          type="checkbox"
        />
        <span>Release transaction plan when approvals complete.</span>
      </label>
    </div>
  );
}

function RepeatingListControl({
  onChange,
  value,
}: {
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const items = stringArrayValue(value);
  const [draft, setDraft] = useState("");

  const addItem = (): void => {
    const item = draft.trim().length > 0 ? draft.trim() : `Item ${items.length + 1}`;
    onChange([...items, item]);
    setDraft("");
  };

  return (
    <div className="repeating-list-control">
      <div className="repeating-list">
        <input
          onChange={(event) => setDraft(event.currentTarget.value)}
          placeholder="List item"
          value={draft}
        />
        <button onClick={addItem} type="button">
          Add
        </button>
      </div>
      <ul>
        {items.map((item, index) => (
          <li key={`${item}-${index}`}>
            <span>{item}</span>
            <button
              onClick={() =>
                onChange(items.filter((_, itemIndex) => itemIndex !== index))
              }
              type="button"
            >
              Remove
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function MatrixControl({
  columns,
  fieldLabel,
  onChange,
  rows,
  value,
}: {
  columns: readonly RecordValue[];
  fieldLabel: string;
  onChange: (value: unknown) => void;
  rows: readonly RecordValue[];
  value: unknown;
}): JSX.Element {
  const rowOptions =
    rows.length > 0
      ? rows
      : [
          { label: "Policy fit", value: "policyFit" },
          { label: "Business impact", value: "businessImpact" },
          { label: "Execution readiness", value: "executionReadiness" },
        ];
  const columnOptions =
    columns.length > 0
      ? columns
      : [
          { label: "Low", value: "low" },
          { label: "Medium", value: "medium" },
          { label: "High", value: "high" },
        ];
  const matrix = objectValue(value);

  return (
    <div
      className="matrix-control"
      style={{
        gridTemplateColumns: `minmax(140px, 1.2fr) repeat(${columnOptions.length}, minmax(92px, 1fr))`,
      }}
    >
      <strong>{fieldLabel}</strong>
      {columnOptions.map((column) => (
        <strong key={optionValue(column)}>{optionLabel(column)}</strong>
      ))}
      {rowOptions.map((row) => {
        const rowValue = optionValue(row);

        return (
          <div className="matrix-row-fragment" key={rowValue}>
            <span>{optionLabel(row)}</span>
            {columnOptions.map((column) => {
              const columnValue = optionValue(column);

              return (
                <label key={columnValue}>
                  <input
                    checked={stringValue(matrix[rowValue]) === columnValue}
                    name={`${fieldLabel}-${rowValue}`}
                    onChange={(event) =>
                      onChange({
                        ...matrix,
                        [rowValue]: event.currentTarget.value,
                      })
                    }
                    type="radio"
                    value={columnValue}
                  />
                  <span className="sr-only">{optionLabel(column)}</span>
                </label>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}

const tableRowsValue = (value: unknown): readonly EditableTableRow[] =>
  Array.isArray(value)
    ? value.filter(isRecord).map((row) => ({
        field: stringValue(row.field),
        value: stringValue(row.value),
      }))
    : [];

function TableEditorControl({
  label,
  onChange,
  value,
}: {
  label: string;
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const rows = tableRowsValue(value);

  const updateRow = (
    rowIndex: number,
    key: keyof EditableTableRow,
    nextValue: string,
  ): void => {
    onChange(
      rows.map((row, index) =>
        index === rowIndex
          ? {
              ...row,
              [key]: nextValue,
            }
          : row,
      ),
    );
  };

  return (
    <div className="table-editor-control">
      <div className="mini-table" role="table">
        <div role="row">
          <span>Field</span>
          <span>Value</span>
          <span>Action</span>
        </div>
        {rows.map((row, index) => (
          <div role="row" key={`${row.field}-${index}`}>
            <input
              aria-label={`${label} field ${index + 1}`}
              onChange={(event) => updateRow(index, "field", event.currentTarget.value)}
              placeholder="Field"
              value={row.field}
            />
            <input
              aria-label={`${label} value ${index + 1}`}
              onChange={(event) => updateRow(index, "value", event.currentTarget.value)}
              placeholder="Value"
              value={row.value}
            />
            <button
              onClick={() => onChange(rows.filter((_, rowIndex) => rowIndex !== index))}
              type="button"
            >
              Remove
            </button>
          </div>
        ))}
      </div>
      <button
        className="secondary-inline-button"
        onClick={() => onChange([...rows, { field: "", value: "" }])}
        type="button"
      >
        Add row
      </button>
    </div>
  );
}

const signaturePointsValue = (value: unknown): readonly SignaturePoint[] =>
  Array.isArray(value)
    ? value.filter(isRecord).map((point) => ({
        x: numberValue(point.x, 0),
        y: numberValue(point.y, 0),
      }))
    : [];

function SignatureCaptureControl({
  id,
  onChange,
  value,
}: {
  id: string;
  onChange: (value: unknown) => void;
  value: unknown;
}): JSX.Element {
  const signature = objectValue(value);
  const method = stringValue(signature.method, "typed");
  const points = signaturePointsValue(signature.points);
  const [drawing, setDrawing] = useState(false);

  const updateSignature = (next: Record<string, unknown>): void => {
    onChange({
      ...signature,
      ...next,
    });
  };

  const addPoint = (event: PointerEvent<HTMLDivElement>): void => {
    const rect = event.currentTarget.getBoundingClientRect();
    const nextPoint = {
      x: Math.round(event.clientX - rect.left),
      y: Math.round(event.clientY - rect.top),
    };
    updateSignature({ points: [...points, nextPoint] });
  };

  return (
    <div className="signature-capture">
      <div className="segmented-control" role="group" aria-label="Signature method">
        {[
          { label: "Type", value: "typed" },
          { label: "Draw", value: "drawn" },
          { label: "Upload", value: "uploaded" },
        ].map((mode) => (
          <button
            aria-pressed={method === mode.value}
            className={method === mode.value ? "segment-active" : ""}
            key={mode.value}
            onClick={() => updateSignature({ method: mode.value })}
            type="button"
          >
            {mode.label}
          </button>
        ))}
      </div>

      {method === "typed" ? (
        <input
          id={id}
          onChange={(event) =>
            updateSignature({ typedName: event.currentTarget.value })
          }
          placeholder="Typed legal name"
          value={stringValue(signature.typedName)}
        />
      ) : null}

      {method === "drawn" ? (
        <div className="signature-draw-area">
          <div
            className="signature-draw-pad"
            onPointerDown={(event) => {
              setDrawing(true);
              event.currentTarget.setPointerCapture(event.pointerId);
              addPoint(event);
            }}
            onPointerMove={(event) => {
              if (drawing) {
                addPoint(event);
              }
            }}
            onPointerUp={() => setDrawing(false)}
            role="img"
            aria-label="Drawn signature pad"
          >
            <svg viewBox="0 0 420 160" preserveAspectRatio="none">
              <polyline
                fill="none"
                points={points.map((point) => `${point.x},${point.y}`).join(" ")}
                stroke="currentColor"
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="3"
              />
            </svg>
          </div>
          <button
            className="secondary-inline-button"
            onClick={() => updateSignature({ points: [] })}
            type="button"
          >
            Clear drawing
          </button>
        </div>
      ) : null}

      {method === "uploaded" ? (
        <div className="file-control">
          <input
            onChange={(event) =>
              updateSignature({
                fileName: event.currentTarget.files?.[0]?.name ?? "",
              })
            }
            type="file"
          />
          <span>
            {stringValue(signature.fileName).length > 0
              ? stringValue(signature.fileName)
              : "No signature file selected"}
          </span>
        </div>
      ) : null}

      <label className="inline-choice">
        <input
          checked={booleanValue(signature.attested)}
          onChange={(event) =>
            updateSignature({ attested: event.currentTarget.checked })
          }
          type="checkbox"
        />
        <span>I attest this signature belongs to the current actor.</span>
      </label>
    </div>
  );
}

function CompositeControl({
  id,
  label,
  onChange,
  type,
  value,
}: {
  id: string;
  label: string;
  onChange: (value: unknown) => void;
  type: string;
  value: unknown;
}): JSX.Element {
  const composite = objectValue(value);

  if (type === "date_range") {
    return (
      <div className="composite-control">
        <input
          aria-label={`${label} start`}
          onChange={(event) =>
            onChange({ ...composite, start: event.currentTarget.value })
          }
          type="date"
          value={stringValue(composite.start)}
        />
        <input
          aria-label={`${label} end`}
          onChange={(event) =>
            onChange({ ...composite, end: event.currentTarget.value })
          }
          type="date"
          value={stringValue(composite.end)}
        />
      </div>
    );
  }

  if (type === "signature" || type === "attestation") {
    return (
      <div className="signature-box">
        <input
          id={id}
          onChange={(event) =>
            onChange({ ...composite, typedName: event.currentTarget.value })
          }
          placeholder="Typed name"
          value={stringValue(composite.typedName)}
        />
        <label className="inline-choice">
          <input
            checked={booleanValue(composite.attested)}
            onChange={(event) =>
              onChange({ ...composite, attested: event.currentTarget.checked })
            }
            type="checkbox"
          />
          <span>I attest this value is accurate.</span>
        </label>
        <span>Signature captured with timestamp and actor identity.</span>
      </div>
    );
  }

  return (
    <div className="composite-control">
      <input
        aria-label={`${label} current`}
        onChange={(event) =>
          onChange({ ...composite, current: event.currentTarget.value })
        }
        placeholder="Current"
        value={stringValue(composite.current)}
      />
      <input
        aria-label={`${label} proposed`}
        onChange={(event) =>
          onChange({ ...composite, proposed: event.currentTarget.value })
        }
        placeholder="Proposed"
        value={stringValue(composite.proposed)}
      />
      <textarea
        aria-label={`${label} notes`}
        onChange={(event) =>
          onChange({ ...composite, notes: event.currentTarget.value })
        }
        placeholder="Notes"
        value={stringValue(composite.notes)}
      />
    </div>
  );
}

function ChangeDiff({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const current = isRecord(widget.bindings.current?.value)
    ? widget.bindings.current.value
    : {};
  const proposed = isRecord(widget.bindings.proposed?.value)
    ? widget.bindings.proposed.value
    : {};
  const keys = Array.from(new Set([...Object.keys(current), ...Object.keys(proposed)]));

  return (
    <div className="diff-table" role="table" aria-label="Current versus proposed">
      <div className="diff-row diff-heading" role="row">
        <span>Field</span>
        <span>Current</span>
        <span>Proposed</span>
      </div>
      {keys.map((key) => (
        <div className="diff-row" role="row" key={key}>
          <span>{key}</span>
          <span>{valueToText(current[key])}</span>
          <span>{valueToText(proposed[key])}</span>
        </div>
      ))}
    </div>
  );
}

function ApprovalPanel({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const [selectedAction, setSelectedAction] = useState<string>(
    widget.instance.actions?.[0]?.action ?? "",
  );

  return (
    <div className="decision-panel">
      <p>
        Decision actions are bound to workflow transitions. Server state and RBAC remain
        authoritative.
      </p>
      <div className="button-row">
        {(widget.instance.actions ?? []).map((action) => (
          <button
            aria-pressed={selectedAction === action.action}
            className={`action-button action-${action.variant ?? "secondary"} ${
              selectedAction === action.action ? "action-selected" : ""
            }`}
            key={action.action}
            onClick={() => setSelectedAction(action.action)}
            type="button"
          >
            {action.label}
          </button>
        ))}
      </div>
      <div className="selected-pill">
        Selected transition: {selectedAction.length > 0 ? selectedAction : "None"}
      </div>
    </div>
  );
}

function SimulationPanel({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const checks = recordsValue(widget.instance.props?.checks);

  return (
    <div className="check-list">
      {checks.map((check) => {
        const status = stringValue(check.status, "info");
        const Icon =
          status === "success"
            ? CheckCircle2
            : status === "warning"
              ? CircleAlert
              : status === "error"
                ? CircleX
                : CircleHelp;

        return (
          <article className="check-row" key={stringValue(check.label)}>
            <Icon size={20} aria-hidden />
            <div>
              <strong>{stringValue(check.label)}</strong>
              <span>{stringValue(check.detail)}</span>
            </div>
            <StatusBadge status={status} />
          </article>
        );
      })}
    </div>
  );
}

function AuditTimeline({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const events = recordsValue(widget.instance.props?.events);

  return (
    <ol className="timeline">
      {events.map((event) => (
        <li key={`${stringValue(event.at)}-${stringValue(event.label)}`}>
          <time>{stringValue(event.at)}</time>
          <strong>{stringValue(event.label)}</strong>
          <span>{stringValue(event.actor)}</span>
        </li>
      ))}
    </ol>
  );
}

function MarkdownViewer({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const markdown = stringValue(widget.instance.props?.markdown);

  return (
    <div className="content-block">
      {markdown.split("\n").map((line) => (
        <p key={line}>{line}</p>
      ))}
    </div>
  );
}

function LinkList({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const links = recordsValue(widget.instance.props?.links);

  return (
    <ul className="link-list">
      {links.map((link) => (
        <li key={stringValue(link.label)}>
          <a href={stringValue(link.href, "#")}>{stringValue(link.label)}</a>
        </li>
      ))}
    </ul>
  );
}

function TextBlock({ widget }: { widget: ResolvedWidget }): JSX.Element {
  return (
    <div className="content-block">
      <p>{stringValue(widget.instance.props?.body)}</p>
      {widget.instance.props?.visibility !== undefined ? (
        <small>{stringValue(widget.instance.props.visibility)}</small>
      ) : null}
    </div>
  );
}

function MetricTile({ widget }: { widget: ResolvedWidget }): JSX.Element {
  return (
    <div className="metric-tile">
      <span>{stringValue(widget.instance.props?.label, widget.instance.title)}</span>
      <strong>{valueToText(widget.instance.props?.value)}</strong>
      <small>{stringValue(widget.instance.props?.detail)}</small>
    </div>
  );
}

function LabelValueList({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const items = recordsValue(widget.instance.props?.items);

  return (
    <dl className="label-value-list">
      {items.map((item) => (
        <div key={stringValue(item.label)}>
          <dt>{stringValue(item.label)}</dt>
          <dd>{valueToText(item.value)}</dd>
        </div>
      ))}
    </dl>
  );
}

function ProgressWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const value = numberValue(widget.instance.props?.value, 0);

  return (
    <div className="progress-widget">
      <progress max="100" value={value} />
      <span>{value}%</span>
    </div>
  );
}

type MetricGraphPoint = {
  label: string;
  value: number;
  target?: number;
};

const metricGraphPointsValue = (value: unknown): readonly MetricGraphPoint[] =>
  recordsValue(value).map((point) => {
    const target = typeof point.target === "number" ? { target: point.target } : {};

    return {
      label: stringValue(point.label, "Metric"),
      value: numberValue(point.value, 0),
      ...target,
    };
  });

function MetricGraphWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const points = metricGraphPointsValue(widget.instance.props?.points);
  const configuredVariant = stringValue(widget.instance.props?.variant, "bar");
  const [variant, setVariant] = useState(
    configuredVariant === "line" || configuredVariant === "bar"
      ? configuredVariant
      : "bar",
  );
  const maxValue = Math.max(
    1,
    ...points.flatMap((point) => [point.value, point.target ?? 0]),
  );
  const pathPoints = points
    .map((point, index) => {
      const x = points.length <= 1 ? 50 : (index / (points.length - 1)) * 100;
      const y = 96 - (point.value / maxValue) * 86;

      return `${x},${y}`;
    })
    .join(" ");

  return (
    <div className="metric-graph-widget">
      <div className="graph-toolbar" role="group" aria-label="Metric graph type">
        {[
          { label: "Bars", value: "bar" },
          { label: "Line", value: "line" },
        ].map((option) => (
          <button
            aria-pressed={variant === option.value}
            className={variant === option.value ? "segment-active" : ""}
            key={option.value}
            onClick={() => setVariant(option.value)}
            type="button"
          >
            {option.label}
          </button>
        ))}
      </div>
      {variant === "bar" ? (
        <div className="metric-bar-chart">
          {points.map((point) => (
            <div className="metric-bar-column" key={point.label}>
              <div className="metric-bar-rail">
                {point.target === undefined ? null : (
                  <span
                    className="metric-target-line"
                    style={{ bottom: `${(point.target / maxValue) * 100}%` }}
                  />
                )}
                <span
                  className="metric-bar"
                  style={{ height: `${(point.value / maxValue) * 100}%` }}
                >
                  {point.value}
                </span>
              </div>
              <small>{point.label}</small>
            </div>
          ))}
        </div>
      ) : (
        <div className="metric-line-chart">
          <svg viewBox="0 0 100 100" preserveAspectRatio="none">
            <polyline points={pathPoints} />
          </svg>
          <div className="metric-line-labels">
            {points.map((point) => (
              <span key={point.label}>
                <strong>{point.value}</strong>
                <small>{point.label}</small>
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

type GraphNode = {
  id: string;
  label: string;
  description: string;
  group: string;
  status: string;
  x: number;
  y: number;
  parentId?: string;
};

type GraphEdge = {
  id: string;
  source: string;
  target: string;
  label: string;
  status: string;
};

const graphNodesValue = (value: unknown): readonly GraphNode[] =>
  recordsValue(value).map((node) => {
    const id = stringValue(node.id, optionValue(node));
    const parent = stringValue(node.parentId, stringValue(node.parent));

    return {
      id,
      label: stringValue(node.label, id),
      description: stringValue(node.description, optionGroup(node)),
      group: optionGroup(node),
      status: stringValue(node.status, "info"),
      x: numberValue(node.x, 50),
      y: numberValue(node.y, 50),
      ...(parent.length > 0 ? { parentId: parent } : {}),
    };
  });

const graphEdgesValue = (value: unknown): readonly GraphEdge[] =>
  recordsValue(value).map((edge, index) => {
    const source = stringValue(edge.source);
    const target = stringValue(edge.target);

    return {
      id: stringValue(edge.id, `${source}-${target}-${index}`),
      source,
      target,
      label: stringValue(edge.label),
      status: stringValue(edge.status, "info"),
    };
  });

function OrgChartWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const nodes = graphNodesValue(widget.instance.props?.nodes);
  const rootNodes = nodes.filter((node) => node.parentId === undefined);
  const [selectedNodeId, setSelectedNodeId] = useState(rootNodes[0]?.id ?? "");

  const renderOrgNode = (node: GraphNode): JSX.Element => {
    const children = nodes.filter((candidate) => candidate.parentId === node.id);
    const selected = selectedNodeId === node.id;

    return (
      <li key={node.id}>
        <button
          aria-pressed={selected}
          className={
            selected ? "org-chart-node org-chart-node-selected" : "org-chart-node"
          }
          onClick={() => setSelectedNodeId(node.id)}
          type="button"
        >
          <span>
            <strong>{node.label}</strong>
            <small>{node.description}</small>
          </span>
          <StatusBadge status={node.status} />
        </button>
        {children.length > 0 ? <ul>{children.map(renderOrgNode)}</ul> : null}
      </li>
    );
  };

  return (
    <div className="org-chart-widget">
      <ul>{rootNodes.map(renderOrgNode)}</ul>
      <div className="selected-pill">
        Selected: {nodes.find((node) => node.id === selectedNodeId)?.label ?? "None"}
      </div>
    </div>
  );
}

function NodeGraphWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const nodes = graphNodesValue(widget.instance.props?.nodes);
  const edges = graphEdgesValue(widget.instance.props?.edges);
  const [selectedNodeId, setSelectedNodeId] = useState(nodes[0]?.id ?? "");
  const nodeMap = new Map(nodes.map((node) => [node.id, node]));

  return (
    <div className="node-graph-widget">
      <div className="node-graph-canvas" role="img" aria-label={widget.instance.title}>
        <svg viewBox="0 0 100 100" preserveAspectRatio="none">
          {edges.map((edge) => {
            const source = nodeMap.get(edge.source);
            const target = nodeMap.get(edge.target);

            if (source === undefined || target === undefined) {
              return null;
            }

            return (
              <g key={edge.id}>
                <line x1={source.x} y1={source.y} x2={target.x} y2={target.y} />
                {edge.label.length > 0 ? (
                  <text x={(source.x + target.x) / 2} y={(source.y + target.y) / 2}>
                    {edge.label}
                  </text>
                ) : null}
              </g>
            );
          })}
        </svg>
        {nodes.map((node) => (
          <button
            aria-pressed={selectedNodeId === node.id}
            className={
              selectedNodeId === node.id
                ? "graph-node graph-node-selected"
                : "graph-node"
            }
            key={node.id}
            onClick={() => setSelectedNodeId(node.id)}
            style={{ left: `${node.x}%`, top: `${node.y}%` }}
            type="button"
          >
            <strong>{node.label}</strong>
            <small>{node.group}</small>
          </button>
        ))}
      </div>
      <div className="selected-pill">
        Selected node: {nodeMap.get(selectedNodeId)?.label ?? "None"}
      </div>
    </div>
  );
}

function GraphChartWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const series = recordsValue(widget.instance.props?.series);
  const [activeSeries, setActiveSeries] = useState(
    stringValue(series[0]?.id, stringValue(series[0]?.label, "series")),
  );
  const activeRecord =
    series.find(
      (item) =>
        stringValue(item.id, stringValue(item.label, "series")) === activeSeries,
    ) ?? series[0];
  const points = metricGraphPointsValue(activeRecord?.points);
  const maxValue = Math.max(1, ...points.map((point) => point.value));
  const pathPoints = points
    .map((point, index) => {
      const x = points.length <= 1 ? 50 : (index / (points.length - 1)) * 100;
      const y = 94 - (point.value / maxValue) * 84;

      return `${x},${y}`;
    })
    .join(" ");

  return (
    <div className="graph-chart-widget">
      <div className="graph-toolbar" role="group" aria-label="Graph chart series">
        {series.map((item) => {
          const id = stringValue(item.id, stringValue(item.label, "series"));

          return (
            <button
              aria-pressed={activeSeries === id}
              className={activeSeries === id ? "segment-active" : ""}
              key={id}
              onClick={() => setActiveSeries(id)}
              type="button"
            >
              {stringValue(item.label, id)}
            </button>
          );
        })}
      </div>
      <div className="graph-chart-canvas">
        <svg viewBox="0 0 100 100" preserveAspectRatio="none">
          <polygon points={`0,100 ${pathPoints} 100,100`} />
          <polyline points={pathPoints} />
          {points.map((point, index) => {
            const x = points.length <= 1 ? 50 : (index / (points.length - 1)) * 100;
            const y = 94 - (point.value / maxValue) * 84;

            return <circle cx={x} cy={y} key={point.label} r="1.7" />;
          })}
        </svg>
        <div className="metric-line-labels">
          {points.map((point) => (
            <span key={point.label}>
              <strong>{point.value}</strong>
              <small>{point.label}</small>
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

function FaqWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const items = recordsValue(widget.instance.props?.items);

  return (
    <div className="faq-list">
      {items.map((item) => (
        <details key={stringValue(item.question)}>
          <summary>{stringValue(item.question)}</summary>
          <p>{stringValue(item.answer)}</p>
        </details>
      ))}
    </div>
  );
}

const tableColumnsValue = (value: unknown): readonly { id: string; label: string }[] =>
  recordsValue(value).map((column) => ({
    id: stringValue(column.id, stringValue(column.value)),
    label: stringValue(column.label, stringValue(column.id)),
  }));

function DataTableWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const columns = tableColumnsValue(widget.instance.props?.columns);
  const rows = recordsValue(widget.instance.props?.rows);

  return (
    <div className="data-table-wrap">
      <table className="data-table">
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column.id}>{column.label}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, rowIndex) => (
            <tr key={`${stringValue(row.id, "row")}-${rowIndex}`}>
              {columns.map((column) => (
                <td key={column.id}>{valueToText(row[column.id])}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function FilterableTableWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const rows = recordsValue(widget.instance.props?.rows);
  const [query, setQuery] = useState("");
  const [riskFilter, setRiskFilter] = useState("all");
  const filteredRows = rows.filter((row) => {
    const queryMatch =
      query.trim().length === 0 ||
      Object.values(row).some((value) =>
        valueToText(value).toLowerCase().includes(query.toLowerCase()),
      );
    const riskMatch =
      riskFilter === "all" || stringValue(row.risk).toLowerCase() === riskFilter;

    return queryMatch && riskMatch;
  });

  return (
    <div className="filterable-table">
      <div className="table-filters">
        <input
          aria-label="Filter table rows"
          onChange={(event) => setQuery(event.currentTarget.value)}
          placeholder="Filter rows"
          value={query}
        />
        <select
          aria-label="Risk filter"
          onChange={(event) => setRiskFilter(event.currentTarget.value)}
          value={riskFilter}
        >
          <option value="all">All risk levels</option>
          <option value="low">Low</option>
          <option value="medium">Medium</option>
          <option value="high">High</option>
        </select>
      </div>
      <DataTableWidget
        widget={{
          ...widget,
          instance: {
            ...widget.instance,
            props: {
              ...widget.instance.props,
              rows: filteredRows,
            },
          },
        }}
      />
      <div className="selected-pill">
        Showing {filteredRows.length} of {rows.length} rows
      </div>
    </div>
  );
}

function MatrixWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const rows = recordsValue(widget.instance.props?.rows);
  const columns = recordsValue(widget.instance.props?.columns);
  const values = objectValue(widget.instance.props?.values);

  return (
    <div
      className="matrix-display"
      style={{
        gridTemplateColumns: `minmax(140px, 1.2fr) repeat(${columns.length}, minmax(86px, 1fr))`,
      }}
    >
      <strong>Criterion</strong>
      {columns.map((column) => (
        <strong key={optionValue(column)}>{optionLabel(column)}</strong>
      ))}
      {rows.map((row) => {
        const rowId = optionValue(row);

        return (
          <div className="matrix-row-fragment" key={rowId}>
            <span>{optionLabel(row)}</span>
            {columns.map((column) => {
              const columnId = optionValue(column);
              const active = stringValue(values[rowId]) === columnId;

              return (
                <span
                  className={active ? "matrix-cell matrix-cell-active" : "matrix-cell"}
                  key={columnId}
                >
                  {active ? "Selected" : ""}
                </span>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}

function ClusterBoardWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const items = recordsValue(widget.instance.props?.items);
  const groups = recordsValue(widget.instance.props?.groups);
  const [clusters, setClusters] = useState<Record<string, unknown>>(() =>
    widget.instance.props?.values === undefined
      ? createClusterState(groups, items)
      : objectValue(widget.instance.props.values),
  );

  return (
    <ClusterBoardControl
      groups={groups}
      items={items}
      label={stringValue(widget.instance.title, "Cluster board")}
      onChange={(value) => setClusters(objectValue(value))}
      value={clusters}
    />
  );
}

function ShowcaseMetrics({
  metrics,
}: {
  metrics: readonly RecordValue[];
}): JSX.Element {
  return (
    <div className="showcase-metrics">
      {metrics.map((metric) => (
        <div key={optionLabel(metric)}>
          <span>{optionLabel(metric)}</span>
          <strong>{valueToText(metric.value)}</strong>
        </div>
      ))}
    </div>
  );
}

function ShowcaseItems({ items }: { items: readonly RecordValue[] }): JSX.Element {
  return (
    <div className="showcase-list">
      {items.map((item) => (
        <article key={`${optionValue(item)}-${optionLabel(item)}`}>
          <span>
            <strong>{optionLabel(item)}</strong>
            <small>{stringValue(item.detail, stringValue(item.description))}</small>
          </span>
          {item.status === undefined ? null : (
            <StatusBadge status={stringValue(item.status, "info")} />
          )}
        </article>
      ))}
    </div>
  );
}

function ShowcaseMessages({
  messages,
}: {
  messages: readonly RecordValue[];
}): JSX.Element {
  return (
    <div className="showcase-messages">
      {messages.map((message, index) => (
        <article key={`${stringValue(message.actor, "message")}-${index}`}>
          <strong>{stringValue(message.actor, `Reviewer ${index + 1}`)}</strong>
          <p>{stringValue(message.body, stringValue(message.detail, "No message"))}</p>
        </article>
      ))}
    </div>
  );
}

function ShowcaseDiff({
  before,
  after,
}: {
  before: string;
  after: string;
}): JSX.Element {
  return (
    <div className="showcase-diff">
      <span>
        <small>Before</small>
        <strong>{before}</strong>
      </span>
      <span>
        <small>After</small>
        <strong>{after}</strong>
      </span>
    </div>
  );
}

function ShowcasePointBars({
  points,
}: {
  points: readonly MetricGraphPoint[];
}): JSX.Element {
  const maxValue = Math.max(1, ...points.map((point) => point.value));

  return (
    <div className="showcase-point-bars">
      {points.map((point) => (
        <span key={point.label}>
          <strong>{point.label}</strong>
          <i style={{ width: `${Math.max(12, (point.value / maxValue) * 100)}%` }} />
          <small>{point.value}</small>
        </span>
      ))}
    </div>
  );
}

function ShowcaseRows({ rows }: { rows: readonly RecordValue[] }): JSX.Element {
  return (
    <div className="showcase-table">
      {rows.map((row, index) => (
        <div key={`${stringValue(row.field, "row")}-${index}`}>
          <strong>{stringValue(row.field, `Row ${index + 1}`)}</strong>
          <span>{valueToText(row.current)}</span>
          <span>{valueToText(row.proposed)}</span>
        </div>
      ))}
    </div>
  );
}

function GenericShowcaseWidget({
  family,
  widget,
}: {
  family: string;
  widget: ResolvedWidget;
}): JSX.Element {
  const props: RecordValue = widget.instance.props ?? {};
  const metrics = recordsValue(props.metrics);
  const items = recordsValue(props.items);
  const messages = recordsValue(props.messages);
  const points = metricGraphPointsValue(props.points);
  const steps = recordsValue(props.steps);
  const rows = recordsValue(props.rows);
  const body = stringValue(props.body, stringValue(props.summary));
  const before = stringValue(props.before);
  const after = stringValue(props.after);
  const code = stringValue(props.code);
  const percent = numberValue(props.percent, numberValue(props.value, 0));

  return (
    <div className={`showcase-widget showcase-${family}`}>
      {body.length > 0 ? <p>{body}</p> : null}
      {percent > 0 ? (
        <div className="showcase-progress">
          <progress max="100" value={Math.min(100, percent)} />
          <strong>{percent}%</strong>
        </div>
      ) : null}
      {metrics.length > 0 ? <ShowcaseMetrics metrics={metrics} /> : null}
      {items.length > 0 ? <ShowcaseItems items={items} /> : null}
      {messages.length > 0 ? <ShowcaseMessages messages={messages} /> : null}
      {points.length > 0 ? <ShowcasePointBars points={points} /> : null}
      {steps.length > 0 ? <ShowcaseItems items={steps} /> : null}
      {rows.length > 0 ? <ShowcaseRows rows={rows} /> : null}
      {before.length > 0 || after.length > 0 ? (
        <ShowcaseDiff before={before || "Not set"} after={after || "Not set"} />
      ) : null}
      {code.length > 0 ? <pre className="showcase-code">{code}</pre> : null}
      {items.length === 0 &&
      steps.length === 0 &&
      metrics.length === 0 &&
      messages.length === 0 &&
      points.length === 0 &&
      rows.length === 0 &&
      body.length === 0 &&
      before.length === 0 &&
      after.length === 0 &&
      code.length === 0 ? (
        <p className="placeholder-text">Widget configured.</p>
      ) : null}
    </div>
  );
}

const chartPercent = (value: number, maxValue: number): number =>
  Math.max(6, Math.min(100, (value / Math.max(1, maxValue)) * 100));

const chartX = (index: number, count: number): number =>
  count <= 1 ? 50 : 8 + (index / (count - 1)) * 84;

const chartY = (value: number, maxValue: number): number =>
  92 - chartPercent(value, maxValue) * 0.78;

function LineChartPreview({
  area,
  combo,
  points,
  stacked,
}: {
  area?: boolean;
  combo?: boolean;
  points: readonly MetricGraphPoint[];
  stacked?: boolean;
}): JSX.Element {
  const maxValue = Math.max(1, ...points.map((point) => point.value));
  const linePoints = points
    .map(
      (point, index) =>
        `${chartX(index, points.length)},${chartY(point.value, maxValue)}`,
    )
    .join(" ");
  const areaPoints = `8,94 ${linePoints} 92,94`;

  return (
    <div
      className={`viz-line-preview${area ? " viz-line-area" : ""}${stacked ? " viz-line-stacked" : ""}${combo ? " viz-line-combo" : ""}`}
    >
      {combo ? (
        <div className="viz-combo-bars" aria-hidden="true">
          {points.map((point) => (
            <i
              key={point.label}
              style={{ height: `${chartPercent(point.value, maxValue)}%` }}
            />
          ))}
        </div>
      ) : null}
      <svg viewBox="0 0 100 100" preserveAspectRatio="none">
        <g className="viz-grid-lines" aria-hidden="true">
          {[26, 50, 74].map((y) => (
            <line key={y} x1="8" x2="92" y1={y} y2={y} />
          ))}
        </g>
        {area ? <polygon points={areaPoints} /> : null}
        {stacked ? (
          <polygon
            className="viz-stacked-area-band"
            points={`8,94 ${points
              .map(
                (point, index) =>
                  `${chartX(index, points.length)},${Math.max(
                    18,
                    chartY(point.value, maxValue) + 14,
                  )}`,
              )
              .join(" ")} 92,94`}
          />
        ) : null}
        <polyline points={linePoints} />
        {points.map((point, index) => (
          <circle
            cx={chartX(index, points.length)}
            cy={chartY(point.value, maxValue)}
            key={point.label}
            r="1.8"
          />
        ))}
      </svg>
      <div className="metric-line-labels">
        {points.map((point) => (
          <span key={point.label}>
            <strong>{point.value}</strong>
            <small>{point.label}</small>
          </span>
        ))}
      </div>
    </div>
  );
}

function BulletChartPreview({
  target,
  value,
}: {
  target: number;
  value: number;
}): JSX.Element {
  return (
    <div className="viz-bullet">
      <span>
        <i style={{ width: `${Math.min(100, value)}%` }} />
        <em style={{ left: `${Math.min(100, target)}%` }} />
      </span>
      <small>
        {value}% of {target}% target
      </small>
    </div>
  );
}

function StackedBarChartPreview({
  grouped,
  points,
}: {
  grouped?: boolean;
  points: readonly MetricGraphPoint[];
}): JSX.Element {
  const maxValue = Math.max(1, ...points.map((point) => point.value));

  return (
    <div className={grouped ? "viz-grouped-bars" : "viz-stacked-bars"}>
      {points.map((point) => (
        <span key={point.label}>
          <strong>{point.label}</strong>
          <span>
            {[0.46, 0.32, 0.22].map((weight, segmentIndex) => (
              <i
                key={`${point.label}-${segmentIndex}`}
                style={{
                  height: grouped
                    ? `${chartPercent(point.value * weight, maxValue)}%`
                    : undefined,
                  width: grouped
                    ? undefined
                    : `${Math.max(10, chartPercent(point.value * weight, maxValue))}%`,
                }}
              />
            ))}
          </span>
          <small>{point.value}</small>
        </span>
      ))}
    </div>
  );
}

function ProportionalChartPreview({
  points,
  variant,
}: {
  points: readonly MetricGraphPoint[];
  variant: string;
}): JSX.Element {
  const colors = [
    "var(--action-primary-background)",
    "var(--status-success)",
    "var(--status-warning)",
    "var(--status-info)",
    "var(--action-danger-background)",
  ];
  const total = Math.max(
    1,
    points.reduce((sum, point) => sum + point.value, 0),
  );
  let start = 0;
  const segments = points.map((point, index) => {
    const end = start + (point.value / total) * 360;
    const segment = `${colors[index % colors.length]} ${start}deg ${end}deg`;
    start = end;
    return segment;
  });

  return (
    <div className={`viz-proportional viz-proportional-${variant}`}>
      <div style={{ background: `conic-gradient(${segments.join(", ")})` }}>
        <strong>{variant.replace("Chart", "")}</strong>
      </div>
      <ul>
        {points.map((point, index) => (
          <li key={point.label}>
            <i style={{ background: colors[index % colors.length] }} />
            <span>{point.label}</span>
            <strong>{point.value}</strong>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ScatterChartPreview({
  points,
  variant,
}: {
  points: readonly MetricGraphPoint[];
  variant: string;
}): JSX.Element {
  const maxValue = Math.max(1, ...points.map((point) => point.value));
  const packed = variant === "packedBubbleChart";
  const bubble = variant === "bubbleChart" || packed;

  return (
    <div className={`viz-scatter viz-scatter-${variant}`}>
      {points.map((point, index) => (
        <span
          key={point.label}
          style={{
            bottom: packed
              ? `${18 + (index % 2) * 24}%`
              : `${Math.min(82, chartPercent(point.value, maxValue) * 0.82)}%`,
            height: bubble
              ? `${34 + chartPercent(point.value, maxValue) * 0.38}px`
              : undefined,
            left: packed ? `${18 + index * 15}%` : `${chartX(index, points.length)}%`,
            width: bubble
              ? `${34 + chartPercent(point.value, maxValue) * 0.38}px`
              : undefined,
          }}
        >
          {bubble ? point.label : ""}
        </span>
      ))}
    </div>
  );
}

function RangeChartPreview({
  points,
  variant,
}: {
  points: readonly MetricGraphPoint[];
  variant: string;
}): JSX.Element {
  const maxValue = Math.max(1, ...points.map((point) => point.value));

  return (
    <div className={`viz-range viz-range-${variant}`}>
      {points.map((point) => {
        const height = chartPercent(point.value, maxValue);
        const low = Math.max(6, height * 0.28);

        return (
          <span key={point.label}>
            <i
              style={{ bottom: `${low}%`, height: `${Math.max(22, height - low)}%` }}
            />
            <em style={{ bottom: `${Math.max(12, height * 0.55)}%` }} />
            <small>{point.label}</small>
          </span>
        );
      })}
    </div>
  );
}

function TimelineChartPreview({
  points,
  variant,
}: {
  points: readonly MetricGraphPoint[];
  variant: string;
}): JSX.Element {
  const maxValue = Math.max(1, ...points.map((point) => point.value));

  return (
    <div className={`viz-timeline-chart viz-timeline-${variant}`}>
      {points.map((point, index) => (
        <span key={point.label}>
          <strong>{point.label}</strong>
          <i
            style={{
              marginLeft: `${index * 7}%`,
              width: `${Math.max(22, chartPercent(point.value, maxValue) * 0.72)}%`,
            }}
          />
        </span>
      ))}
    </div>
  );
}

function VisualizationShowcaseWidget({
  widget,
}: {
  widget: ResolvedWidget;
}): JSX.Element {
  const props: RecordValue = widget.instance.props ?? {};
  const configuredPoints = metricGraphPointsValue(props.points);
  const rows = recordsValue(props.rows);
  const value = numberValue(props.value, 72);
  const target = numberValue(props.target, 85);
  const variant = widget.instance.type.replace("viz.", "");
  const points =
    configuredPoints.length > 0
      ? configuredPoints
      : [
          { label: "Current", value },
          { label: "Target", value: target },
        ];
  const maxValue = Math.max(1, target, ...points.map((point) => point.value));

  if (variant === "cohortTable" || variant === "pivotTable" || variant === "crossTab") {
    return <ShowcaseRows rows={rows} />;
  }

  if (variant === "gauge") {
    return (
      <div className="viz-widget">
        <div
          className="viz-gauge"
          style={{
            background: `conic-gradient(var(--action-primary-background) ${value * 3.6}deg, var(--surface-subtle) 0deg)`,
          }}
        >
          <strong>{value}%</strong>
          <span>Target {target}%</span>
        </div>
      </div>
    );
  }

  if (variant === "lineChart" || variant === "sparkline") {
    return <LineChartPreview points={points} />;
  }

  if (variant === "areaChart" || variant === "stackedAreaChart") {
    return (
      <LineChartPreview area points={points} stacked={variant === "stackedAreaChart"} />
    );
  }

  if (variant === "comboChart") {
    return <LineChartPreview combo points={points} />;
  }

  if (variant === "bulletChart") {
    return <BulletChartPreview target={target} value={value} />;
  }

  if (variant === "horizontalBarChart") {
    return <ShowcasePointBars points={points} />;
  }

  if (variant === "stackedBarChart" || variant === "groupedBarChart") {
    return (
      <StackedBarChartPreview grouped={variant === "groupedBarChart"} points={points} />
    );
  }

  if (
    variant === "pieChart" ||
    variant === "donutChart" ||
    variant === "polarAreaChart" ||
    variant === "sunburstChart"
  ) {
    return <ProportionalChartPreview points={points} variant={variant} />;
  }

  if (
    variant === "scatterPlot" ||
    variant === "bubbleChart" ||
    variant === "packedBubbleChart"
  ) {
    return <ScatterChartPreview points={points} variant={variant} />;
  }

  if (variant === "boxPlot" || variant === "candlestickChart") {
    return <RangeChartPreview points={points} variant={variant} />;
  }

  if (variant === "timelineChart" || variant === "ganttChart") {
    return <TimelineChartPreview points={points} variant={variant} />;
  }

  if (variant === "funnel") {
    return (
      <div className="viz-funnel">
        {points.map((point, index) => (
          <span
            key={point.label}
            style={{
              width: `${Math.max(32, 100 - index * 14)}%`,
            }}
          >
            <strong>{point.label}</strong>
            <small>{point.value}</small>
          </span>
        ))}
      </div>
    );
  }

  if (
    variant === "heatmap" ||
    variant === "calendarHeatmap" ||
    variant === "treemap" ||
    variant === "regionMap"
  ) {
    return (
      <div className={`viz-tiles viz-tiles-${variant}`}>
        {points.concat(points).map((point, index) => (
          <span
            key={`${point.label}-${index}`}
            style={{
              opacity: 0.28 + Math.min(0.7, point.value / maxValue),
            }}
          >
            {variant === "regionMap" ? point.label : point.value}
          </span>
        ))}
      </div>
    );
  }

  if (variant === "radar") {
    return (
      <div className="viz-radar">
        {points.map((point, index) => (
          <span
            key={point.label}
            style={{
              transform: `rotate(${index * (360 / points.length)}deg) translateY(-72px)`,
            }}
          >
            {point.label}
          </span>
        ))}
        <strong>Balanced</strong>
      </div>
    );
  }

  if (variant === "sankey") {
    return (
      <div className="viz-sankey">
        {points.slice(0, 4).map((point, index) => (
          <span key={point.label} style={{ gridColumn: `${index + 1}` }}>
            {point.label}
          </span>
        ))}
      </div>
    );
  }

  return (
    <div className={`viz-bars viz-bars-${variant}`}>
      {points.map((point) => (
        <span key={point.label}>
          <i style={{ height: `${(point.value / maxValue) * 100}%` }} />
          <small>{point.label}</small>
        </span>
      ))}
    </div>
  );
}

const padClockPart = (value: number): string =>
  String(Math.trunc(Math.abs(value))).padStart(2, "0");

const clockPartsForOffset = (
  now: Date,
  offsetMinutes: number,
): { hours: number; minutes: number; seconds: number } => {
  const shifted = new Date(now.getTime() + offsetMinutes * 60_000);

  return {
    hours: shifted.getUTCHours(),
    minutes: shifted.getUTCMinutes(),
    seconds: shifted.getUTCSeconds(),
  };
};

const formatClockTime = (now: Date, offsetMinutes: number): string => {
  const parts = clockPartsForOffset(now, offsetMinutes);

  return `${padClockPart(parts.hours)}:${padClockPart(parts.minutes)}:${padClockPart(parts.seconds)}`;
};

const formatDuration = (totalSeconds: number): string => {
  const safeSeconds = Math.max(0, Math.trunc(totalSeconds));
  const hours = Math.floor(safeSeconds / 3600);
  const minutes = Math.floor((safeSeconds % 3600) / 60);
  const seconds = safeSeconds % 60;

  return `${padClockPart(hours)}:${padClockPart(minutes)}:${padClockPart(seconds)}`;
};

const formatUtcOffset = (offsetMinutes: number): string => {
  const sign = offsetMinutes >= 0 ? "+" : "-";
  const absolute = Math.abs(offsetMinutes);

  return `UTC${sign}${padClockPart(Math.floor(absolute / 60))}:${padClockPart(absolute % 60)}`;
};

function TimeControls({
  onReset,
  onToggle,
  running,
}: {
  onReset: () => void;
  onToggle: () => void;
  running: boolean;
}): JSX.Element {
  return (
    <div className="time-controls">
      <button onClick={onToggle} type="button">
        {running ? "Pause" : "Start"}
      </button>
      <button onClick={onReset} type="button">
        Reset
      </button>
    </div>
  );
}

function TimeRing({
  label,
  progress,
  status,
  value,
}: {
  label: string;
  progress: number;
  status?: string | undefined;
  value: string;
}): JSX.Element {
  return (
    <div
      className="time-ring"
      style={{
        background: `conic-gradient(var(--action-primary-background) ${
          Math.min(100, Math.max(0, progress)) * 3.6
        }deg, var(--surface-subtle) 0deg)`,
      }}
    >
      <strong>{value}</strong>
      <span>{label}</span>
      {status === undefined ? null : <small>{status}</small>}
    </div>
  );
}

function TimeShowcaseWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const props: RecordValue = widget.instance.props ?? {};
  const variant = widget.instance.type.replace("time.", "");
  const label = stringValue(props.label, widget.instance.title);
  const offsetMinutes = numberValue(props.offsetMinutes, 0);
  const durationSeconds = numberValue(props.durationSeconds, 900);
  const zones = recordsValue(props.zones);
  const items = recordsValue(props.items);
  const [now, setNow] = useState(() => new Date());
  const [timer, setTimer] = useState(() => ({
    accumulated: numberValue(props.elapsedSeconds, 0),
    running: props.running === false ? false : true,
    startedAt: Date.now(),
  }));

  useEffect(() => {
    const interval = globalThis.setInterval(() => setNow(new Date()), 1000);

    return () => globalThis.clearInterval(interval);
  }, []);

  const elapsedSeconds =
    timer.accumulated +
    (timer.running ? Math.floor((now.getTime() - timer.startedAt) / 1000) : 0);
  const remainingSeconds = Math.max(0, durationSeconds - elapsedSeconds);
  const progress = Math.min(100, (elapsedSeconds / Math.max(1, durationSeconds)) * 100);

  const toggleTimer = (): void => {
    setTimer((current) => {
      const accumulated =
        current.accumulated +
        (current.running ? Math.floor((Date.now() - current.startedAt) / 1000) : 0);

      return {
        accumulated,
        running: !current.running,
        startedAt: Date.now(),
      };
    });
  };

  const resetTimer = (): void => {
    setTimer({
      accumulated: numberValue(props.elapsedSeconds, 0),
      running: true,
      startedAt: Date.now(),
    });
  };

  if (variant === "analogClock") {
    const parts = clockPartsForOffset(now, offsetMinutes);
    const hourAngle = ((parts.hours % 12) + parts.minutes / 60) * 30;
    const minuteAngle = (parts.minutes + parts.seconds / 60) * 6;
    const secondAngle = parts.seconds * 6;

    return (
      <div className="time-widget time-clock-widget">
        <div className="time-widget-header">
          <strong>{label}</strong>
          <small>{formatUtcOffset(offsetMinutes)}</small>
        </div>
        <div className="time-analog-face">
          <i
            className="time-hand-hour"
            style={{ transform: `rotate(${hourAngle}deg)` }}
          />
          <i
            className="time-hand-minute"
            style={{ transform: `rotate(${minuteAngle}deg)` }}
          />
          <i
            className="time-hand-second"
            style={{ transform: `rotate(${secondAngle}deg)` }}
          />
          <span>12</span>
          <span>3</span>
          <span>6</span>
          <span>9</span>
        </div>
      </div>
    );
  }

  if (variant === "digitalClock") {
    return (
      <div className="time-widget time-digital-widget">
        <div className="time-widget-header">
          <strong>{label}</strong>
          <small>{formatUtcOffset(offsetMinutes)}</small>
        </div>
        <output>{formatClockTime(now, offsetMinutes)}</output>
      </div>
    );
  }

  if (variant === "worldClock") {
    return (
      <div className="time-widget time-world-clock">
        {(zones.length > 0 ? zones : [{ label, offsetMinutes }]).map((zone) => {
          const zoneOffset = numberValue(zone.offsetMinutes, 0);

          return (
            <article key={optionLabel(zone)}>
              <span>
                <strong>{optionLabel(zone)}</strong>
                <small>{stringValue(zone.detail, formatUtcOffset(zoneOffset))}</small>
              </span>
              <output>{formatClockTime(now, zoneOffset)}</output>
            </article>
          );
        })}
      </div>
    );
  }

  if (variant === "timezoneCompare") {
    return (
      <div className="time-widget time-zone-compare">
        <p>{stringValue(props.body, "Compare approver working hours.")}</p>
        {(zones.length > 0 ? zones : [{ label, offsetMinutes }]).map((zone) => {
          const zoneOffset = numberValue(zone.offsetMinutes, 0);
          const localHour = clockPartsForOffset(now, zoneOffset).hours;
          const inWindow = localHour >= 9 && localHour < 17;

          return (
            <article key={optionLabel(zone)}>
              <strong>{optionLabel(zone)}</strong>
              <span>
                <i
                  style={{ left: `${(9 / 24) * 100}%`, width: `${(8 / 24) * 100}%` }}
                />
                <em style={{ left: `${(localHour / 24) * 100}%` }} />
              </span>
              <StatusBadge status={inWindow ? "success" : "warning"} />
            </article>
          );
        })}
      </div>
    );
  }

  if (variant === "countdownRing" || variant === "slaTimer") {
    return (
      <div className={`time-widget time-ring-widget time-ring-${variant}`}>
        <TimeRing
          label={label}
          progress={variant === "slaTimer" ? 100 - progress : progress}
          status={variant === "slaTimer" ? "SLA remaining" : undefined}
          value={formatDuration(remainingSeconds)}
        />
        <TimeControls
          onReset={resetTimer}
          onToggle={toggleTimer}
          running={timer.running}
        />
      </div>
    );
  }

  if (variant === "countdownTimer") {
    return (
      <div className="time-widget time-countdown-widget">
        <div className="time-widget-header">
          <strong>{label}</strong>
          <small>{Math.round(100 - progress)}% remaining</small>
        </div>
        <output>{formatDuration(remainingSeconds)}</output>
        <progress max="100" value={100 - progress} />
        <TimeControls
          onReset={resetTimer}
          onToggle={toggleTimer}
          running={timer.running}
        />
      </div>
    );
  }

  if (variant === "stopwatch" || variant === "durationTimer") {
    return (
      <div className="time-widget time-stopwatch-widget">
        <div className="time-widget-header">
          <strong>{label}</strong>
          <small>
            {variant === "durationTimer" ? "Elapsed duration" : "Stopwatch"}
          </small>
        </div>
        <output>{formatDuration(elapsedSeconds)}</output>
        {variant === "durationTimer" ? <progress max="100" value={progress} /> : null}
        <TimeControls
          onReset={resetTimer}
          onToggle={toggleTimer}
          running={timer.running}
        />
      </div>
    );
  }

  if (variant === "lapTimer") {
    return (
      <div className="time-widget time-lap-widget">
        <div className="time-widget-header">
          <strong>{label}</strong>
          <small>{formatDuration(elapsedSeconds)}</small>
        </div>
        <ShowcaseItems
          items={[
            ...items,
            { label: `Current lap`, detail: formatDuration(elapsedSeconds % 300) },
          ]}
        />
        <TimeControls
          onReset={resetTimer}
          onToggle={toggleTimer}
          running={timer.running}
        />
      </div>
    );
  }

  if (variant === "pomodoroTimer" || variant === "intervalTimer") {
    const phases =
      items.length > 0
        ? items
        : [
            { label: "Focus", detail: "25 min" },
            { label: "Break", detail: "5 min" },
          ];
    const phaseIndex =
      Math.floor(elapsedSeconds / Math.max(1, durationSeconds)) % phases.length;
    const phaseElapsed = elapsedSeconds % Math.max(1, durationSeconds);
    const activePhase = phases[phaseIndex] ?? phases[0] ?? { label: "Focus" };

    return (
      <div className="time-widget time-interval-widget">
        <TimeRing
          label={optionLabel(activePhase)}
          progress={(phaseElapsed / Math.max(1, durationSeconds)) * 100}
          value={formatDuration(durationSeconds - phaseElapsed)}
        />
        <ShowcaseItems items={phases} />
        <TimeControls
          onReset={resetTimer}
          onToggle={toggleTimer}
          running={timer.running}
        />
      </div>
    );
  }

  if (variant === "businessHoursWindow") {
    const localHour = clockPartsForOffset(now, offsetMinutes).hours;
    const open = localHour >= 9 && localHour < 17;

    return (
      <div className="time-widget time-business-hours">
        <div className="time-widget-header">
          <strong>{label}</strong>
          <StatusBadge status={open ? "success" : "warning"} />
        </div>
        <output>{formatClockTime(now, offsetMinutes)}</output>
        <ShowcaseItems items={items} />
      </div>
    );
  }

  if (variant === "cronSchedule" || variant === "relativeTime") {
    return (
      <div className="time-widget time-schedule-widget">
        <div className="time-widget-header">
          <strong>{label}</strong>
          <small>{variant === "cronSchedule" ? "Next runs" : "Relative age"}</small>
        </div>
        <ShowcaseItems items={items} />
      </div>
    );
  }

  return <GenericShowcaseWidget family="time" widget={widget} />;
}

function UiBlockShowcaseWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const props: RecordValue = widget.instance.props ?? {};
  const items = recordsValue(props.items);
  const steps = recordsValue(props.steps);
  const before = stringValue(props.before);
  const after = stringValue(props.after);
  const blockType = widget.instance.type.replace("ui.", "");

  if (blockType === "resizableSplitPane") {
    return <ShowcaseDiff before={before} after={after} />;
  }

  return (
    <div className={`ui-block-preview ui-block-${blockType}`}>
      {(steps.length > 0 ? steps : items).map((item, index) => (
        <span key={`${optionLabel(item)}-${index}`}>
          <strong>{optionLabel(item)}</strong>
          <small>
            {stringValue(
              item.detail,
              stringValue(item.description, `Item ${index + 1}`),
            )}
          </small>
        </span>
      ))}
      {items.length === 0 && steps.length === 0 ? (
        <p>{stringValue(props.body, "Reusable generated UI surface.")}</p>
      ) : null}
    </div>
  );
}

function ESignatureCanvasWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const props: RecordValue = widget.instance.props ?? {};
  const [signature, setSignature] = useState<unknown>(
    () => props.value ?? { method: "drawn", attested: false, points: [] },
  );
  const captured = objectValue(signature);
  const actor = stringValue(props.actor, "Current actor");
  const reason = stringValue(props.reason, "Workflow attestation");

  return (
    <div className="e-signature-widget">
      <div className="e-signature-header">
        <span>
          <strong>{actor}</strong>
          <small>{reason}</small>
        </span>
        <StatusBadge status={booleanValue(captured.attested) ? "success" : "warning"} />
      </div>
      <SignatureCaptureControl
        id={`${widget.instance.id}-signature`}
        onChange={setSignature}
        value={signature}
      />
    </div>
  );
}

function PdfViewerWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const src = stringValue(widget.instance.props?.src);
  const label = stringValue(widget.instance.props?.label, "PDF preview");
  const pageCount = numberValue(widget.instance.props?.pageCount, 4);
  const [page, setPage] = useState(1);
  const [zoom, setZoom] = useState(100);

  return (
    <div className="pdf-viewer">
      <div className="pdf-toolbar" aria-label="PDF viewer controls">
        <button
          disabled={page <= 1}
          onClick={() => setPage((current) => Math.max(1, current - 1))}
          type="button"
        >
          Previous
        </button>
        <span>
          Page {page} of {pageCount}
        </span>
        <button
          disabled={page >= pageCount}
          onClick={() => setPage((current) => Math.min(pageCount, current + 1))}
          type="button"
        >
          Next
        </button>
        <button
          onClick={() => setZoom((current) => Math.max(75, current - 25))}
          type="button"
        >
          Zoom out
        </button>
        <span>{zoom}%</span>
        <button
          onClick={() => setZoom((current) => Math.min(150, current + 25))}
          type="button"
        >
          Zoom in
        </button>
      </div>
      {src.length > 0 ? (
        <iframe className="pdf-frame" src={src} title={widget.instance.title} />
      ) : (
        <div className="pdf-page" style={{ transform: `scale(${zoom / 100})` }}>
          <strong>{label}</strong>
          <span>Page {page}</span>
          <p>
            Simulated document preview for policies, signed packets, offer letters,
            pay-band guidance, and uploaded evidence.
          </p>
        </div>
      )}
    </div>
  );
}

function MediaWidget({ widget }: { widget: ResolvedWidget }): JSX.Element {
  const src = stringValue(widget.instance.props?.src);
  const alt = stringValue(widget.instance.props?.alt, widget.instance.title);
  const transcript = stringValue(widget.instance.props?.transcript);

  if (widget.instance.type === "media.image") {
    return <img className="media-image" src={src} alt={alt} />;
  }

  if (widget.instance.type === "media.audio") {
    return src.length > 0 ? (
      <audio controls src={src}>
        {transcript}
      </audio>
    ) : (
      <p className="placeholder-text">{transcript}</p>
    );
  }

  if (widget.instance.type === "media.video") {
    return src.length > 0 ? (
      <video controls src={src}>
        {transcript}
      </video>
    ) : (
      <p className="placeholder-text">{transcript}</p>
    );
  }

  return <PdfViewerWidget widget={widget} />;
}

function WidgetBody({
  fieldBrandingStyleProps,
  widget,
  widgetBrandingStyleProps,
}: {
  fieldBrandingStyleProps?: FieldControlBaseStyleProps;
  widget: ResolvedWidget;
  widgetBrandingStyleProps?: WidgetStyleProps;
}): JSX.Element {
  const libraryWidgetBody = renderWidgetFromRegistry(
    widget,
    undefined,
    widgetBrandingStyleProps,
  );

  if (libraryWidgetBody !== undefined) {
    return libraryWidgetBody;
  }

  if (widget.instance.type === "queue.requestList") {
    return <RequestQueue widget={widget} />;
  }

  if (widget.instance.type === "employee.summary") {
    return <EmployeeSummary widget={widget} />;
  }

  if (widget.instance.type === "form.dynamicFieldGroup") {
    return (
      <DynamicFieldGroup
        {...(fieldBrandingStyleProps === undefined
          ? {}
          : { brandingStyleProps: fieldBrandingStyleProps })}
        widget={widget}
      />
    );
  }

  if (widget.instance.type === "change.diff") {
    return <ChangeDiff widget={widget} />;
  }

  if (widget.instance.type === "approval.decisionPanel") {
    return <ApprovalPanel widget={widget} />;
  }

  if (widget.instance.type === "simulation.resultPanel") {
    return <SimulationPanel widget={widget} />;
  }

  if (widget.instance.type === "audit.timeline") {
    return <AuditTimeline widget={widget} />;
  }

  if (widget.instance.type === "content.markdown") {
    return <MarkdownViewer widget={widget} />;
  }

  if (widget.instance.type === "content.text") {
    return <TextBlock widget={widget} />;
  }

  if (widget.instance.type === "content.html") {
    return (
      <div
        className="content-block"
        dangerouslySetInnerHTML={{
          __html: sanitizeHtmlSubset(stringValue(widget.instance.props?.html)),
        }}
      />
    );
  }

  if (widget.instance.type === "content.callout") {
    return <p className="callout-body">{stringValue(widget.instance.props?.body)}</p>;
  }

  if (widget.instance.type === "content.linkList") {
    return <LinkList widget={widget} />;
  }

  if (widget.instance.type === "content.metricTile") {
    return <MetricTile widget={widget} />;
  }

  if (widget.instance.type === "content.labelValueList") {
    return <LabelValueList widget={widget} />;
  }

  if (widget.instance.type === "data.progress") {
    return <ProgressWidget widget={widget} />;
  }

  if (widget.instance.type === "data.metricGraph") {
    return <MetricGraphWidget widget={widget} />;
  }

  if (widget.instance.type === "data.graphChart") {
    return <GraphChartWidget widget={widget} />;
  }

  if (widget.instance.type === "data.nodeGraph") {
    return <NodeGraphWidget widget={widget} />;
  }

  if (widget.instance.type === "data.orgChart") {
    return <OrgChartWidget widget={widget} />;
  }

  if (widget.instance.type === "data.table") {
    return <DataTableWidget widget={widget} />;
  }

  if (widget.instance.type === "data.filterableTable") {
    return <FilterableTableWidget widget={widget} />;
  }

  if (widget.instance.type === "data.matrix") {
    return <MatrixWidget widget={widget} />;
  }

  if (widget.instance.type === "data.clusterBoard") {
    return <ClusterBoardWidget widget={widget} />;
  }

  if (widget.instance.type.startsWith("hcm.")) {
    return <GenericShowcaseWidget family="hcm" widget={widget} />;
  }

  if (widget.instance.type.startsWith("workflow.")) {
    if (widget.instance.type === "workflow.dependencyGraph") {
      return <NodeGraphWidget widget={widget} />;
    }

    return <GenericShowcaseWidget family="workflow" widget={widget} />;
  }

  if (widget.instance.type.startsWith("viz.")) {
    return <VisualizationShowcaseWidget widget={widget} />;
  }

  if (widget.instance.type.startsWith("time.")) {
    return <TimeShowcaseWidget widget={widget} />;
  }

  if (widget.instance.type.startsWith("document.")) {
    if (widget.instance.type === "document.eSignatureCanvas") {
      return <ESignatureCanvasWidget widget={widget} />;
    }

    return <GenericShowcaseWidget family="document" widget={widget} />;
  }

  if (widget.instance.type.startsWith("collaboration.")) {
    return <GenericShowcaseWidget family="collaboration" widget={widget} />;
  }

  if (widget.instance.type.startsWith("ai.")) {
    return <GenericShowcaseWidget family="ai" widget={widget} />;
  }

  if (widget.instance.type.startsWith("integration.")) {
    return <GenericShowcaseWidget family="integration" widget={widget} />;
  }

  const widgetFamily = widget.instance.type.split(".")[0] ?? "";

  if (hrDomainWidgetFamilies.has(widgetFamily)) {
    return <GenericShowcaseWidget family={widgetFamily} widget={widget} />;
  }

  if (widget.instance.type.startsWith("ui.")) {
    return <UiBlockShowcaseWidget widget={widget} />;
  }

  if (widget.instance.type === "content.faq") {
    return <FaqWidget widget={widget} />;
  }

  if (widget.instance.type.startsWith("media.")) {
    return <MediaWidget widget={widget} />;
  }

  return (
    <div className="content-block">
      <p>
        {stringValue(
          widget.instance.props?.body,
          stringValue(widget.instance.props?.summary, "Widget configured."),
        )}
      </p>
      {widget.instance.props?.visibility !== undefined ? (
        <small>{stringValue(widget.instance.props.visibility)}</small>
      ) : null}
    </div>
  );
}

export function WorkflowPageRenderer({
  fieldBrandingStyleProps,
  page,
  runtimeContext,
  widgetBrandingStyleProps,
}: WorkflowPageRendererProps): JSX.Element {
  const resolvedPage = useMemo(
    () => resolvePage(page, runtimeContext),
    [page, runtimeContext],
  );

  if (!resolvedPage.visible) {
    return (
      <div className="empty-state">
        This page is not available for the selected surface.
      </div>
    );
  }

  // Prefer the intent declared on the generated page itself (e.g. "employee.
  // termination" when the AI generated a termination form) over whatever was
  // pinned in the static runtime context — otherwise an AI page rendered while
  // the demo context is on a different workflow would submit to the wrong one.
  const pageDeclaredIntent =
    Array.isArray(page.workflowTypes) &&
    page.workflowTypes.length > 0 &&
    page.workflowTypes[0] !== "*"
      ? page.workflowTypes[0]
      : undefined;
  const runtimeContextIntent =
    typeof runtimeContext.workflow.type === "string" &&
    runtimeContext.workflow.type.length > 0
      ? runtimeContext.workflow.type
      : undefined;
  const workflowIntent = pageDeclaredIntent ?? runtimeContextIntent;
  // The demo runtime context doesn't carry `subjectType`; the only configured
  // subject for HR workflows today is `worker`, so default to that when the
  // context doesn't declare otherwise. The submit hook only sends subjectType
  // when present, and the server validates it against the workflow config.
  const contextSubjectType = stringValueFromUnknown(
    (runtimeContext.workflow.config as Record<string, unknown> | undefined)?.[
      "subjectType"
    ],
  );
  const workflowSubjectType = contextSubjectType ?? "worker";
  const actorId =
    typeof runtimeContext.actor.id === "string" && runtimeContext.actor.id.length > 0
      ? runtimeContext.actor.id
      : undefined;

  return (
    <PageFormProvider
      {...(workflowIntent === undefined
        ? {}
        : { initialWorkflowIntent: workflowIntent })}
      {...(workflowSubjectType === undefined
        ? {}
        : { initialWorkflowSubjectType: workflowSubjectType })}
      {...(actorId === undefined ? {} : { initialActorId: actorId })}
    >
      <PageFormContextActorSync actorId={actorId} />
      <div className="workflow-page" style={resolvedPage.cssVariables}>
        <PageFormSubmitNotice />
        <header className="page-header">
          <div>
            <p className="eyebrow">{runtimeContext.tenant.name}</p>
            <h1>{resolvedPage.page.title}</h1>
            <p>{resolvedPage.page.description}</p>
          </div>
          <div className="page-meta" aria-label="Current workflow context">
            <span>{runtimeContext.workflow.state}</span>
            <strong>{runtimeContext.actor.displayName}</strong>
          </div>
        </header>

        {resolvedPage.regions.map((region) => (
          <section
            className={`page-region region-${region.region.layout} region-${region.region.width}`}
            key={region.region.id}
          >
            {region.widgets.map((widget) => (
              <WidgetShell
                {...(widgetBrandingStyleProps === undefined
                  ? {}
                  : { brandingStyleProps: widgetBrandingStyleProps })}
                widget={widget}
                key={widget.instance.id}
              >
                <WidgetBody
                  {...(fieldBrandingStyleProps === undefined
                    ? {}
                    : { fieldBrandingStyleProps })}
                  widget={widget}
                  {...(widgetBrandingStyleProps === undefined
                    ? {}
                    : { widgetBrandingStyleProps })}
                />
              </WidgetShell>
            ))}
          </section>
        ))}
      </div>
    </PageFormProvider>
  );
}

const stringValueFromUnknown = (value: unknown): string | undefined =>
  typeof value === "string" && value.length > 0 ? value : undefined;

/**
 * Mirrors any later actorId change from the renderer's props into the form
 * provider. The initial value comes through `initialActorId`; this keeps
 * provider state in sync for the rare hot-swap case (logout/login).
 */
function PageFormContextActorSync({
  actorId,
}: {
  actorId: string | undefined;
}): JSX.Element | null {
  const pageForm = usePageForm();
  useEffect(() => {
    pageForm.setActorId(actorId);
  }, [actorId, pageForm]);
  return null;
}

/**
 * Renders a callout at the top of the page when a workflow has been started
 * from the action bar's Submit. Keeps the confirmation visible even when the
 * action bar itself scrolls off-screen.
 */
function PageFormSubmitNotice(): JSX.Element | null {
  const pageForm = usePageForm();
  const result = pageForm.submitResult;
  if (result === undefined) {
    return null;
  }
  const interactionSuffix =
    result.currentInteraction === undefined
      ? ""
      : ` · interaction: ${result.currentInteraction}`;
  return (
    <div className="callout-body status-success" role="status">
      <strong>Workflow started.</strong>{" "}
      <span>
        Started {result.workflowInstanceId} · state: {result.currentState}
        {interactionSuffix}
      </span>
    </div>
  );
}
