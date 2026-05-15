import {
  booleanValue,
  isControlRecord,
  numberValue,
  readRecord,
  stringValue,
} from "./guards";
import { controlErr, controlOk, type ControlResult } from "./result";
import { readDensity, readTone, readVariant } from "./style";
import type {
  ControlDataConfig,
  ControlDisplayConfig,
  ControlRecord,
  FieldControlConfig,
} from "./types";

const readLayout = (value: unknown): ControlDisplayConfig["layout"] => {
  if (
    value === "stack" ||
    value === "grid" ||
    value === "inline" ||
    value === "cards" ||
    value === "table"
  ) {
    return value;
  }

  return undefined;
};

const createDataConfig = (
  field: ControlRecord,
  data: ControlRecord,
): ControlDataConfig => ({
  source: stringValue(data.source, stringValue(field.source)),
  path: stringValue(data.path, stringValue(field.path)),
  itemsPath: stringValue(data.itemsPath, stringValue(field.itemsPath)),
  valuePath: stringValue(data.valuePath, "value"),
  labelPath: stringValue(data.labelPath, "label"),
  groupPath: stringValue(data.groupPath, "group"),
  descriptionPath: stringValue(data.descriptionPath, "description"),
  statusPath: stringValue(data.statusPath, "status"),
  scopePath: stringValue(data.scopePath, "scope"),
  permissionPath: stringValue(data.permissionPath, "permission"),
  disabledPath: stringValue(data.disabledPath, "disabled"),
  metadataPath: stringValue(data.metadataPath, "metadata"),
  emptyLabel: stringValue(data.emptyLabel, "No matching options"),
});

/**
 * Parses an AI/workflow field record into the stable generated-control contract.
 */
export const parseFieldControlConfig = (
  field: ControlRecord,
): ControlResult<FieldControlConfig> => {
  const data = readRecord(field, "data");
  const display = readRecord(field, "display");
  const style = readRecord(field, "style");
  const appearance = readRecord(field, "appearance");
  const behavior = readRecord(field, "behavior");
  const validation = readRecord(field, "validation");
  const id = stringValue(field.id);
  const label = stringValue(field.label, id);
  const dataConfig = createDataConfig(field, data);
  const displayEmptyLabel = stringValue(
    display.emptyLabel,
    stringValue(data.emptyLabel),
  );
  const aiInstruction = stringValue(field.aiInstruction);

  if (id.length === 0) {
    return controlErr("CONTROL_CONFIG_INVALID_FIELD", "Control id is required.");
  }

  return controlOk({
    id,
    label,
    type: stringValue(field.type, "text"),
    required: booleanValue(field.required),
    placeholder: stringValue(field.placeholder, label),
    help: stringValue(field.help),
    data: dataConfig,
    display: {
      layout: readLayout(display.layout),
      showPreview:
        display.showPreview === undefined
          ? undefined
          : booleanValue(display.showPreview),
      showConstraints:
        display.showConstraints === undefined
          ? undefined
          : booleanValue(display.showConstraints),
      emptyLabel: displayEmptyLabel,
    },
    style: {
      tone: readTone(style.tone ?? appearance.tone),
      variant: readVariant(style.variant ?? appearance.variant),
      density: readDensity(style.density ?? appearance.density),
      accentColor: stringValue(style.accentColor),
      surfaceColor: stringValue(style.surfaceColor),
      mutedSurfaceColor: stringValue(style.mutedSurfaceColor),
      activeSurfaceColor: stringValue(style.activeSurfaceColor),
      borderColor: stringValue(style.borderColor),
      borderStrongColor: stringValue(style.borderStrongColor),
      textColor: stringValue(style.textColor),
      mutedTextColor: stringValue(style.mutedTextColor),
      radius: stringValue(style.radius),
      controlHeight: stringValue(style.controlHeight),
      shadow: stringValue(style.shadow),
      hoverLift: stringValue(style.hoverLift),
      hoverScale: stringValue(style.hoverScale),
      motionFast: stringValue(style.motionFast),
      motionMedium: stringValue(style.motionMedium),
    },
    behavior: {
      searchEnabled:
        behavior.searchEnabled === undefined
          ? undefined
          : booleanValue(behavior.searchEnabled),
      multiSelect:
        behavior.multiSelect === undefined
          ? undefined
          : booleanValue(behavior.multiSelect),
      allowCustomValue:
        behavior.allowCustomValue === undefined
          ? undefined
          : booleanValue(behavior.allowCustomValue),
      clearable:
        behavior.clearable === undefined ? undefined : booleanValue(behavior.clearable),
      readonly:
        behavior.readonly === undefined ? undefined : booleanValue(behavior.readonly),
      disabled:
        behavior.disabled === undefined ? undefined : booleanValue(behavior.disabled),
    },
    validation: {
      required:
        validation.required === undefined
          ? undefined
          : booleanValue(validation.required),
      min: numberValue(validation.min),
      max: numberValue(validation.max),
      minLength: numberValue(validation.minLength),
      maxLength: numberValue(validation.maxLength),
      pattern: stringValue(validation.pattern),
      message: stringValue(validation.message),
    },
    aiInstruction: aiInstruction.length > 0 ? aiInstruction : undefined,
    raw: field,
  });
};

/**
 * Validates unknown workflow/AI config input before parsing a generated control.
 */
export const parseUnknownFieldControlConfig = (
  field: unknown,
): ControlResult<FieldControlConfig> => {
  if (!isControlRecord(field)) {
    return controlErr(
      "CONTROL_CONFIG_INVALID_RECORD",
      "Control config must be an object.",
    );
  }

  return parseFieldControlConfig(field);
};

/**
 * Builds a parameterized control config while keeping current renderer calls simple.
 */
export const createFieldControlConfig = (field: ControlRecord): FieldControlConfig => {
  const parsedConfig = parseFieldControlConfig(field);

  if (parsedConfig.ok) {
    return parsedConfig.value;
  }

  const fallbackId = stringValue(field.id, "generated_control");

  return {
    id: fallbackId,
    label: stringValue(field.label, fallbackId),
    type: stringValue(field.type, "text"),
    required: booleanValue(field.required),
    placeholder: stringValue(field.placeholder, fallbackId),
    help: parsedConfig.error.safeMessage,
    data: createDataConfig(field, {}),
    display: {},
    style: {
      tone: "danger",
      variant: "outlined",
      density: "default",
    },
    behavior: {},
    validation: {},
    raw: field,
  };
};
