import { controlErr, controlOk, type ControlResult } from "./result";
import { isControlRecord, isControlRecordArray } from "./guards";
import { readControlPath } from "./path";
import type {
  ControlDataConfig,
  ControlDataSources,
  ControlOption,
  ControlRecord,
  ControlRecordMappingConfig,
} from "./types";

const resolvedOptionField = (
  record: ControlRecord,
  path: string | undefined,
  fallbackKey: string,
): unknown => {
  const pathValue = readControlPath(record, path);

  return pathValue === undefined ? record[fallbackKey] : pathValue;
};

/**
 * Normalizes arbitrary AI/API records into the label/value/group shape controls use.
 */
export const normalizeControlRecord = (
  record: ControlRecord,
  mapping: ControlRecordMappingConfig,
): ControlOption => ({
  ...record,
  value: resolvedOptionField(record, mapping.valuePath, "value"),
  label: resolvedOptionField(record, mapping.labelPath, "label"),
  group: resolvedOptionField(record, mapping.groupPath, "group"),
  description: resolvedOptionField(record, mapping.descriptionPath, "description"),
  status: resolvedOptionField(record, mapping.statusPath, "status"),
  scope: resolvedOptionField(record, mapping.scopePath, "scope"),
  permission: resolvedOptionField(record, mapping.permissionPath, "permission"),
  disabled: resolvedOptionField(record, mapping.disabledPath, "disabled"),
  metadata: resolvedOptionField(record, mapping.metadataPath, "metadata"),
});

/**
 * Converts unknown data into normalized options with explicit parse failure.
 */
export const parseControlRecords = (
  records: unknown,
  mapping: ControlRecordMappingConfig,
): ControlResult<readonly ControlOption[]> => {
  if (!isControlRecordArray(records)) {
    return controlErr(
      "CONTROL_CONFIG_INVALID_RECORD_LIST",
      "Control records must be an array of objects.",
    );
  }

  return controlOk(records.map((record) => normalizeControlRecord(record, mapping)));
};

const selectSourceValue = (
  dataSources: ControlDataSources,
  data: ControlDataConfig,
): unknown => {
  const sourceName = data.source ?? "";

  if (sourceName.length === 0) {
    return dataSources;
  }

  return dataSources[sourceName];
};

const selectRecordsValue = (sourceValue: unknown, data: ControlDataConfig): unknown => {
  const recordsPath = data.itemsPath ?? data.path;

  if (recordsPath === undefined || recordsPath.length === 0) {
    return sourceValue;
  }

  if (isControlRecord(sourceValue)) {
    return readControlPath(sourceValue, recordsPath);
  }

  return undefined;
};

/**
 * Resolves and normalizes records from named workflow/API data sources.
 */
export const parseControlRecordsFromSources = (
  dataSources: ControlDataSources,
  data: ControlDataConfig,
): ControlResult<readonly ControlOption[]> => {
  const sourceValue = selectSourceValue(dataSources, data);
  const recordsValue = selectRecordsValue(sourceValue, data);

  return parseControlRecords(recordsValue, data);
};

/**
 * Compatibility helper for existing renderers that already filter record arrays.
 */
export const normalizeControlRecords = (
  records: readonly ControlRecord[],
  data: ControlDataConfig,
): readonly ControlOption[] =>
  records.map((record) => normalizeControlRecord(record, data));
