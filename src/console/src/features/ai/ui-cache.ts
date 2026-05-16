import { fromThrowable } from "@hcm-next/foundation";
import type { PageDefinition } from "@hcm-next/ui-contracts";

/**
 * Composite identity for a cached AI-generated page. Order-significant for
 * `refinementSequence` (each entry represents a refinement turn applied on
 * top of the previous one).
 */
export type UiCacheKey = {
  workflowConfigHash: string;
  currentState: string;
  actorRole: string;
  refinementSequence: readonly string[];
};

/**
 * Persisted cache entry shape. `storedAt` lets us evict oldest on overflow
 * and surface debug info. `workflowConfigHash` is duplicated on the entry
 * for future schema-aware invalidation (e.g. drop all entries whose hash
 * is stale relative to the live registry).
 */
export type UiCacheEntry = {
  page: PageDefinition;
  storedAt: string;
  workflowConfigHash: string;
};

const storageKey = "hcm-next:ai-ui-cache:v1";
const maxEntries = 50;

type CacheRecord = Record<string, UiCacheEntry>;

/**
 * Indicates the localStorage boundary failed in a way the caller should
 * silently tolerate (SSR, quota exceeded, disabled storage, corrupt JSON).
 */
type StorageError = {
  kind: "storage_unavailable" | "parse_failed" | "stringify_failed" | "write_failed";
};

const storageError = (kind: StorageError["kind"]): StorageError => ({ kind });

const hasBrowserStorage = (): boolean => {
  return typeof window !== "undefined" && typeof window.localStorage !== "undefined";
};

const readRawStorage = () =>
  fromThrowable(
    () => window.localStorage.getItem(storageKey),
    () => storageError("storage_unavailable"),
  );

const writeRawStorage = (serialized: string) =>
  fromThrowable(
    () => {
      window.localStorage.setItem(storageKey, serialized);
    },
    () => storageError("write_failed"),
  );

const removeRawStorage = () =>
  fromThrowable(
    () => {
      window.localStorage.removeItem(storageKey);
    },
    () => storageError("storage_unavailable"),
  );

const parseJson = (raw: string) =>
  fromThrowable(
    () => JSON.parse(raw) as unknown,
    () => storageError("parse_failed"),
  );

const stringifyJson = (value: CacheRecord) =>
  fromThrowable(
    () => JSON.stringify(value),
    () => storageError("stringify_failed"),
  );

/**
 * Type guard for a single persisted entry. Defensive against unexpected
 * shapes (corrupted writes, schema drift, hand-edited values).
 */
const isCacheEntry = (value: unknown): value is UiCacheEntry => {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.storedAt === "string" &&
    typeof candidate.workflowConfigHash === "string" &&
    typeof candidate.page === "object" &&
    candidate.page !== null
  );
};

const isCacheRecord = (value: unknown): value is CacheRecord => {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  return Object.values(value as Record<string, unknown>).every(isCacheEntry);
};

/**
 * Build a stable canonical string for a `UiCacheKey`. Top-level fields are
 * sorted alphabetically so future field additions cannot reorder the input.
 * `refinementSequence` is order-significant and is preserved verbatim.
 */
const canonicalizeKey = (key: UiCacheKey): string => {
  const canonical = {
    actorRole: key.actorRole,
    currentState: key.currentState,
    refinementSequence: [...key.refinementSequence],
    workflowConfigHash: key.workflowConfigHash,
  };
  return JSON.stringify(canonical);
};

const toHex = (buffer: ArrayBuffer): string => {
  const bytes = new Uint8Array(buffer);
  let hex = "";
  for (const byte of bytes) {
    hex += byte.toString(16).padStart(2, "0");
  }
  return hex;
};

/**
 * SHA-1 hex digest of the canonical `UiCacheKey`. SHA-1 is sufficient: this
 * is a cache key, not a security primitive, and we want short, fast digests.
 */
export async function signatureForKey(key: UiCacheKey): Promise<string> {
  const canonical = canonicalizeKey(key);
  const data = new TextEncoder().encode(canonical);
  const digest = await crypto.subtle.digest("SHA-1", data);
  return toHex(digest);
}

const loadRecord = (): CacheRecord => {
  if (!hasBrowserStorage()) {
    return {};
  }
  const rawResult = readRawStorage();
  if (!rawResult.ok || rawResult.value === null) {
    return {};
  }
  const parsed = parseJson(rawResult.value);
  if (!parsed.ok) {
    // Corrupt JSON: drop the whole blob so subsequent reads succeed.
    removeRawStorage();
    return {};
  }
  if (!isCacheRecord(parsed.value)) {
    removeRawStorage();
    return {};
  }
  return parsed.value;
};

const persistRecord = (record: CacheRecord): void => {
  if (!hasBrowserStorage()) {
    return;
  }
  const serialized = stringifyJson(record);
  if (!serialized.ok) {
    return;
  }
  // Quota errors are silently tolerated: missing the cache is non-fatal.
  writeRawStorage(serialized.value);
};

/**
 * Evict the oldest entries (by `storedAt`) until the record fits the cap.
 * Stable: ties resolved by signature ordering so behaviour is deterministic
 * in tests.
 */
const enforceCap = (record: CacheRecord): CacheRecord => {
  const entries = Object.entries(record);
  if (entries.length <= maxEntries) {
    return record;
  }
  const sorted = [...entries].sort((left, right) => {
    if (left[1].storedAt === right[1].storedAt) {
      return left[0].localeCompare(right[0]);
    }
    return left[1].storedAt.localeCompare(right[1].storedAt);
  });
  const trimmed = sorted.slice(entries.length - maxEntries);
  const next: CacheRecord = {};
  for (const [signature, entry] of trimmed) {
    next[signature] = entry;
  }
  return next;
};

/**
 * Look up a cached page by composite key. Returns undefined for SSR, cache
 * miss, or any boundary failure (corrupt JSON, disabled storage). Never
 * throws.
 */
export async function getCachedPage(
  key: UiCacheKey,
): Promise<PageDefinition | undefined> {
  if (!hasBrowserStorage()) {
    return undefined;
  }
  const signature = await signatureForKey(key);
  const record = loadRecord();
  const entry = record[signature];
  return entry ? entry.page : undefined;
}

/**
 * Persist a generated page under the composite key. No-ops cleanly on SSR
 * or storage failures (quota, disabled storage). Enforces the 50-entry cap
 * by evicting the oldest entry on overflow.
 */
export async function setCachedPage(
  key: UiCacheKey,
  page: PageDefinition,
): Promise<void> {
  if (!hasBrowserStorage()) {
    return;
  }
  const signature = await signatureForKey(key);
  const record = loadRecord();
  record[signature] = {
    page,
    storedAt: new Date().toISOString(),
    workflowConfigHash: key.workflowConfigHash,
  };
  const capped = enforceCap(record);
  persistRecord(capped);
}

/**
 * Drop every cached entry. Used by the panel "Clear cached views" affordance.
 */
export function clearCachedPages(): void {
  if (!hasBrowserStorage()) {
    return;
  }
  removeRawStorage();
}

/**
 * Debug-oriented list of stored entries (signature + storedAt only). Page
 * payloads are intentionally omitted to keep the surface small.
 */
export function listCachedEntries(): readonly {
  signature: string;
  storedAt: string;
}[] {
  if (!hasBrowserStorage()) {
    return [];
  }
  const record = loadRecord();
  return Object.entries(record).map(([signature, entry]) => ({
    signature,
    storedAt: entry.storedAt,
  }));
}
