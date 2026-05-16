import {
  controlClassNames,
  controlStyleProps,
  displayValue,
  eventInputValue,
  objectFieldValue,
  optionDescription,
  optionLabel,
  optionLevel,
  optionPermission,
  optionScope,
  optionStatus,
  optionValue,
  rawNumber,
  recordValue,
  scalarInputValue,
  updateObjectField,
} from "./utils";
import { SelectControl } from "./choice-controls";
import type { FieldControlProps } from "./types";

const hcmEditorFieldsByType: Readonly<Record<string, readonly string[]>> = {
  compensation_editor: ["basePay", "currency", "frequency", "reason"],
  job_change_editor: ["jobProfile", "jobCode", "level", "title"],
  manager_org_change_editor: ["managerId", "orgUnitId", "departmentId", "costCenterId"],
  worker_assignment_editor: ["workerType", "fte", "locationId", "payGroup"],
  role_binding_editor: ["role", "scope", "effectiveDate", "expiresOn"],
  effective_dated_fact_editor: [
    "factType",
    "currentValue",
    "proposedValue",
    "effectiveDate",
  ],
};

const statusClassName = (status: string): string => `field-library-status-${status}`;

export const EntityPickerControl = (props: FieldControlProps) => (
  <div
    className={controlClassNames(props, "field-library-entity-picker")}
    style={controlStyleProps(props)}
  >
    {(props.options ?? []).map((option) => {
      const value = optionValue(option);
      const isSelected = scalarInputValue(props.value) === value;

      return (
        <button
          aria-pressed={isSelected}
          className={`field-library-entity-card ${statusClassName(optionStatus(option))}`}
          key={value}
          onClick={() => props.onChange(value)}
          type="button"
        >
          <span>
            {optionLabel(option)}
            <small>{optionDescription(option)}</small>
          </span>
          <span className="field-library-entity-meta">
            {optionScope(option)}
            {optionPermission(option).length > 0 ? (
              <code>{optionPermission(option)}</code>
            ) : null}
          </span>
        </button>
      );
    })}
  </div>
);

export const TreePickerControl = (props: FieldControlProps) => (
  <div
    className={controlClassNames(props, "field-library-tree-picker")}
    style={controlStyleProps(props)}
  >
    {(props.options ?? []).map((option) => {
      const value = optionValue(option);
      const level = optionLevel(option);

      return (
        <button
          aria-pressed={scalarInputValue(props.value) === value}
          className={`field-library-tree-node ${statusClassName(optionStatus(option))}`}
          key={value}
          onClick={() => props.onChange(value)}
          style={{ paddingInlineStart: `${Math.max(level, 0) * 1.25 + 0.75}rem` }}
          type="button"
        >
          <span>{optionLabel(option)}</span>
          <small>{optionDescription(option)}</small>
        </button>
      );
    })}
  </div>
);

export const HcmRecordEditorControl = (props: FieldControlProps) => {
  const editorFields = hcmEditorFieldsByType[props.config.type] ?? ["field", "value"];
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-hcm-record-editor")}
      style={controlStyleProps(props)}
    >
      {editorFields.map((fieldName) => (
        <label className="field-library-stack-row" key={fieldName}>
          <span>{fieldName}</span>
          <input
            className="field-library-input"
            onChange={(event) =>
              props.onChange(
                updateObjectField(currentValue, fieldName, eventInputValue(event)),
              )
            }
            value={displayValue(currentValue[fieldName], "")}
          />
        </label>
      ))}
    </div>
  );
};

export const EffectiveDatedChangeControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-effective-dated-change")}
      style={controlStyleProps(props)}
    >
      {["effectiveDate", "dateMode", "payrollCutoff", "reason"].map((fieldName) => (
        <label className="field-library-stack-row" key={fieldName}>
          <span>{fieldName}</span>
          <input
            className="field-library-input"
            onChange={(event) =>
              props.onChange(
                updateObjectField(currentValue, fieldName, eventInputValue(event)),
              )
            }
            type={fieldName === "effectiveDate" ? "date" : "text"}
            value={objectFieldValue(currentValue, fieldName)}
          />
        </label>
      ))}
    </div>
  );
};

export const BeforeAfterFieldEditorControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);
  const currentFieldValue =
    objectFieldValue(currentValue, "current") ||
    displayValue(props.config.raw.current, "");
  const proposedFieldValue =
    objectFieldValue(currentValue, "proposed") ||
    displayValue(props.config.raw.proposed, "");

  return (
    <div
      className={controlClassNames(props, "field-library-before-after-editor")}
      style={controlStyleProps(props)}
    >
      <label>
        <span>Current</span>
        <input className="field-library-input" readOnly value={currentFieldValue} />
      </label>
      <label>
        <span>Proposed</span>
        <input
          className="field-library-input"
          onChange={(event) =>
            props.onChange(
              updateObjectField(currentValue, "proposed", eventInputValue(event)),
            )
          }
          value={proposedFieldValue}
        />
      </label>
    </div>
  );
};

export const CompensationPackageEditorControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);
  const defaultBasePay = rawNumber(props.config, "basePay", 0);
  const defaultBonusTarget = rawNumber(props.config, "bonusTarget", 0);

  return (
    <div
      className={controlClassNames(props, "field-library-comp-package-editor")}
      style={controlStyleProps(props)}
    >
      {(
        [
          ["basePay", "number", defaultBasePay],
          ["currency", "text", "USD"],
          ["payFrequency", "text", "annual"],
          ["bonusTarget", "number", defaultBonusTarget],
          ["allowance", "number", 0],
        ] satisfies readonly (readonly [string, string, string | number])[]
      ).map(([fieldName, inputType, fallbackValue]) => (
        <label className="field-library-stack-row" key={String(fieldName)}>
          <span>{fieldName}</span>
          <input
            className="field-library-input"
            onChange={(event) =>
              props.onChange(
                updateObjectField(
                  currentValue,
                  String(fieldName),
                  inputType === "number"
                    ? Number(eventInputValue(event))
                    : eventInputValue(event),
                ),
              )
            }
            type={String(inputType)}
            value={displayValue(currentValue[String(fieldName)] ?? fallbackValue, "")}
          />
        </label>
      ))}
    </div>
  );
};

export const ScheduleTimeControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-schedule-time")}
      style={controlStyleProps(props)}
    >
      {(
        [
          ["fte", "number"],
          ["workSchedule", "text"],
          ["timeZone", "text"],
          ["startTime", "time"],
          ["endTime", "time"],
        ] satisfies readonly (readonly [string, string])[]
      ).map(([fieldName, inputType]) => (
        <label className="field-library-stack-row" key={fieldName}>
          <span>{fieldName}</span>
          <input
            className="field-library-input"
            onChange={(event) =>
              props.onChange(
                updateObjectField(currentValue, fieldName, eventInputValue(event)),
              )
            }
            type={inputType}
            value={objectFieldValue(currentValue, fieldName)}
          />
        </label>
      ))}
    </div>
  );
};

export const InternationalContactControl = (props: FieldControlProps) => {
  const currentValue = recordValue(props.value);

  return (
    <div
      className={controlClassNames(props, "field-library-international-contact")}
      style={controlStyleProps(props)}
    >
      {[
        "country",
        "addressLine1",
        "addressLine2",
        "city",
        "region",
        "postalCode",
        "phone",
      ].map((fieldName) => (
        <label className="field-library-stack-row" key={fieldName}>
          <span>{fieldName}</span>
          <input
            className="field-library-input"
            onChange={(event) =>
              props.onChange(
                updateObjectField(currentValue, fieldName, eventInputValue(event)),
              )
            }
            value={objectFieldValue(currentValue, fieldName)}
          />
        </label>
      ))}
    </div>
  );
};

export const HcmControl = (props: FieldControlProps) => {
  if (
    props.config.type === "tree_picker" ||
    props.config.type === "manager_tree_picker" ||
    props.config.type === "org_tree_picker"
  ) {
    return <TreePickerControl {...props} />;
  }

  if (props.config.type === "effective_dated_change") {
    return <EffectiveDatedChangeControl {...props} />;
  }

  if (props.config.type === "before_after_field_editor") {
    return <BeforeAfterFieldEditorControl {...props} />;
  }

  if (props.config.type === "compensation_package_editor") {
    return <CompensationPackageEditorControl {...props} />;
  }

  if (props.config.type === "schedule_time_control") {
    return <ScheduleTimeControl {...props} />;
  }

  if (props.config.type === "international_contact") {
    return <InternationalContactControl {...props} />;
  }

  if (props.config.type.endsWith("_editor")) {
    return <HcmRecordEditorControl {...props} />;
  }

  if (props.options !== undefined && props.options.length > 0) {
    return props.config.type === "payroll_cutoff_picker" ? (
      <SelectControl {...props} />
    ) : (
      <EntityPickerControl {...props} />
    );
  }

  return <HcmRecordEditorControl {...props} />;
};
