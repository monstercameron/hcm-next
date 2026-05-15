import type { JsonObject, JsonValue } from "../domain";

export function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function asJsonObject(value: unknown): JsonObject {
  return isJsonObject(value) ? value : {};
}

export function valuesAreEqual(left: JsonValue, right: JsonValue): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

export function cloneJsonObject<T extends JsonObject>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}
