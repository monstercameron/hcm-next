import type { ControlRecord } from "./types";

/** Checks whether an unknown value is a plain record usable by generated controls. */
export const isControlRecord = (value: unknown): value is ControlRecord =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/** Checks whether an unknown value is a list of plain records. */
export const isControlRecordArray = (
  value: unknown,
): value is readonly ControlRecord[] =>
  Array.isArray(value) && value.every(isControlRecord);

/** Reads a string from unknown generated config input. */
export const stringValue = (value: unknown, fallback = ""): string =>
  typeof value === "string" ? value : fallback;

/** Reads a strict boolean from unknown generated config input. */
export const booleanValue = (value: unknown): boolean =>
  typeof value === "boolean" ? value : false;

/** Reads a strict optional boolean from unknown generated config input. */
export const optionalBooleanValue = (value: unknown): boolean | undefined =>
  typeof value === "boolean" ? value : undefined;

/** Reads a finite optional number from unknown generated config input. */
export const numberValue = (value: unknown): number | undefined =>
  typeof value === "number" && Number.isFinite(value) ? value : undefined;

/** Reads a child record from a generated config object. */
export const readRecord = (record: ControlRecord, key: string): ControlRecord =>
  isControlRecord(record[key]) ? record[key] : {};
