import type {
  ChangeEvent,
  CSSProperties,
  InputHTMLAttributes,
  TextareaHTMLAttributes,
} from "react";
import type { ControlRecord, FieldControlConfig } from "../../control-config";
import type { FieldControlBaseStyleProps, FieldControlProps } from "./types";

export const isRecord = (value: unknown): value is ControlRecord =>
  typeof value === "object" && value !== null && !Array.isArray(value);

export const stringValue = (value: unknown, fallback = ""): string =>
  typeof value === "string" ? value : fallback;

export const numberValue = (value: unknown, fallback: number): number =>
  typeof value === "number" ? value : fallback;

export const booleanValue = (value: unknown): boolean =>
  typeof value === "boolean" ? value : false;

export const recordsValue = (value: unknown): readonly ControlRecord[] =>
  Array.isArray(value) ? value.filter(isRecord) : [];

export const stringArrayValue = (value: unknown): readonly string[] =>
  Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];

export const recordValue = (value: unknown): ControlRecord =>
  isRecord(value) ? value : {};

export const mergeClassNames = (
  ...classNames: readonly (string | undefined | false)[]
): string => classNames.filter(Boolean).join(" ");

export const mergeFieldControlStyleProps = (
  brandingStyleProps: FieldControlBaseStyleProps | undefined,
  localStyleProps: FieldControlBaseStyleProps,
): FieldControlBaseStyleProps => ({
  className: mergeClassNames(brandingStyleProps?.className, localStyleProps.className),
  contentClassName: mergeClassNames(
    brandingStyleProps?.contentClassName,
    localStyleProps.contentClassName,
  ),
  controlClassName: mergeClassNames(
    brandingStyleProps?.controlClassName,
    localStyleProps.controlClassName,
  ),
  cssVariables: {
    ...brandingStyleProps?.cssVariables,
    ...localStyleProps.cssVariables,
  },
  style: {
    ...brandingStyleProps?.style,
    ...localStyleProps.style,
  },
});

export const controlClassNames = (
  props: FieldControlProps,
  baseClassName: string,
): string => {
  const mergedStyleProps = mergeFieldControlStyleProps(props.brandingStyleProps, props);

  return mergeClassNames(
    baseClassName,
    mergedStyleProps.className,
    mergedStyleProps.controlClassName,
    "field-library-control",
    `field-library-control-${props.config.type}`,
    `field-library-tone-${props.config.style.tone}`,
    `field-library-variant-${props.config.style.variant}`,
    `field-library-density-${props.config.style.density}`,
  );
};

export const controlStyleProps = (props: FieldControlProps): CSSProperties => {
  const mergedStyleProps = mergeFieldControlStyleProps(props.brandingStyleProps, props);

  return {
    ...mergedStyleProps.cssVariables,
    ...mergedStyleProps.style,
  };
};

export const optionValue = (option: ControlRecord): string =>
  stringValue(option.value, stringValue(option.label));

export const optionLabel = (option: ControlRecord): string =>
  stringValue(option.label, optionValue(option));

export const optionGroup = (option: ControlRecord): string =>
  stringValue(option.group, "Other");

export const optionDescription = (option: ControlRecord): string =>
  stringValue(option.description);

export const optionStatus = (option: ControlRecord): string =>
  stringValue(option.status, "neutral");

export const optionScope = (option: ControlRecord): string => stringValue(option.scope);

export const optionPermission = (option: ControlRecord): string =>
  stringValue(option.permission);

export const optionLevel = (option: ControlRecord): number =>
  numberValue(option.level, 0);

export const groupedOptions = (
  options: readonly ControlRecord[],
): readonly { group: string; options: readonly ControlRecord[] }[] => {
  const groupedRecords = new Map<string, ControlRecord[]>();

  for (const option of options) {
    const group = optionGroup(option);
    const existingOptions = groupedRecords.get(group) ?? [];
    groupedRecords.set(group, [...existingOptions, option]);
  }

  return Array.from(groupedRecords.entries()).map(([group, groupOptions]) => ({
    group,
    options: groupOptions,
  }));
};

export const optionMatchesQuery = (option: ControlRecord, query: string): boolean => {
  const normalizedQuery = query.trim().toLowerCase();

  if (normalizedQuery.length === 0) {
    return true;
  }

  return [
    optionLabel(option),
    optionValue(option),
    optionGroup(option),
    optionDescription(option),
  ].some((candidate) => candidate.toLowerCase().includes(normalizedQuery));
};

export const rawString = (
  config: FieldControlConfig,
  key: string,
  fallback = "",
): string => stringValue(config.raw[key], fallback);

export const rawNumber = (
  config: FieldControlConfig,
  key: string,
  fallback: number,
): number => numberValue(config.raw[key], fallback);

export const rawBoolean = (config: FieldControlConfig, key: string): boolean =>
  booleanValue(config.raw[key]);

export const scalarInputValue = (value: unknown): string => {
  if (typeof value === "string" || typeof value === "number") {
    return String(value);
  }

  return "";
};

export const displayValue = (value: unknown, fallback = "Not set"): string => {
  if (typeof value === "string") {
    return value.length === 0 ? fallback : value;
  }

  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }

  if (value === undefined || value === null) {
    return fallback;
  }

  return JSON.stringify(value);
};

export const objectFieldValue = (value: unknown, key: string, fallback = ""): string =>
  stringValue(recordValue(value)[key], fallback);

export const updateObjectField = (
  value: unknown,
  key: string,
  nextValue: unknown,
): ControlRecord => ({
  ...recordValue(value),
  [key]: nextValue,
});

export const updateIndexedRecord = (
  records: readonly ControlRecord[],
  index: number,
  key: string,
  nextValue: unknown,
): readonly ControlRecord[] =>
  records.map((record, recordIndex) =>
    recordIndex === index ? { ...record, [key]: nextValue } : record,
  );

export const inputConstraintAttributes = (
  config: FieldControlConfig,
): InputHTMLAttributes<HTMLInputElement> => {
  const field = config.raw;
  const pattern = stringValue(field.pattern);
  const validationMessage = stringValue(field.validationMessage);
  const inputMode = stringValue(field.inputMode);
  const autoComplete = stringValue(field.autoComplete);
  const min = field.min;
  const max = field.max;
  const step = field.step;
  const minLength = field.minLength;
  const maxLength = field.maxLength;

  return {
    ...(config.required ? { required: true } : {}),
    ...(rawBoolean(config, "readOnly") ? { readOnly: true } : {}),
    ...(rawBoolean(config, "disabled") ? { disabled: true } : {}),
    ...(pattern.length > 0 ? { pattern } : {}),
    ...(validationMessage.length > 0 ? { title: validationMessage } : {}),
    ...(inputMode.length > 0
      ? {
          inputMode: inputMode as InputHTMLAttributes<HTMLInputElement>["inputMode"],
        }
      : {}),
    ...(autoComplete.length > 0 ? { autoComplete } : {}),
    ...(typeof min === "string" || typeof min === "number" ? { min } : {}),
    ...(typeof max === "string" || typeof max === "number" ? { max } : {}),
    ...(typeof step === "number" ? { step } : {}),
    ...(typeof minLength === "number" ? { minLength } : {}),
    ...(typeof maxLength === "number" ? { maxLength } : {}),
  };
};

export const textareaConstraintAttributes = (
  config: FieldControlConfig,
): TextareaHTMLAttributes<HTMLTextAreaElement> => {
  const field = config.raw;
  const minLength = field.minLength;
  const maxLength = field.maxLength;

  return {
    ...(config.required ? { required: true } : {}),
    ...(rawBoolean(config, "readOnly") ? { readOnly: true } : {}),
    ...(rawBoolean(config, "disabled") ? { disabled: true } : {}),
    ...(typeof minLength === "number" ? { minLength } : {}),
    ...(typeof maxLength === "number" ? { maxLength } : {}),
  };
};

export const selectedStringSet = (value: unknown): ReadonlySet<string> =>
  new Set(stringArrayValue(value));

export const nextCheckedStringArray = (
  currentValue: unknown,
  option: string,
  isChecked: boolean,
): readonly string[] => {
  const currentValues = new Set(stringArrayValue(currentValue));

  if (isChecked) {
    currentValues.add(option);
  } else {
    currentValues.delete(option);
  }

  return Array.from(currentValues);
};

export const eventInputValue = (
  event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>,
): string => event.currentTarget.value;
