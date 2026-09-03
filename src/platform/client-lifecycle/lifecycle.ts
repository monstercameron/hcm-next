/** Backend-neutral security and lifecycle primitives for browser clients. */

export type Asset = { path: string; sha256: string; size: number };

export type ReleaseManifest = {
  releaseId: string;
  schemaVersion: string;
  keyId: string;
  signature: string;
  assets: readonly Asset[];
  serviceWorker: { version: string; sha256: string };
  csp: { nonceRequired: boolean; trustedTypesPolicy: string };
};

export type ManifestProblem =
  | "missing_release_identity"
  | "unsigned_release"
  | "untrusted_key"
  | "duplicate_asset"
  | "invalid_asset_digest"
  | "invalid_asset_size"
  | "service_worker_downgrade"
  | "service_worker_digest_mismatch"
  | "weak_csp"
  | "invalid_trusted_types_policy";

export type ManifestValidation = { ok: true } | { ok: false; problems: readonly ManifestProblem[] };

const digest = /^[a-f0-9]{64}$/;

export function validateReleaseManifest(
  manifest: ReleaseManifest,
  trustedKeyIds: ReadonlySet<string>,
  installedServiceWorkerVersion?: string,
): ManifestValidation {
  const problems: ManifestProblem[] = [];
  if (!manifest.releaseId || !manifest.schemaVersion) problems.push("missing_release_identity");
  if (!manifest.signature) problems.push("unsigned_release");
  if (!trustedKeyIds.has(manifest.keyId)) problems.push("untrusted_key");
  const paths = new Set<string>();
  for (const asset of manifest.assets) {
    if (paths.has(asset.path)) problems.push("duplicate_asset");
    paths.add(asset.path);
    if (!digest.test(asset.sha256)) problems.push("invalid_asset_digest");
    if (!Number.isSafeInteger(asset.size) || asset.size < 0) problems.push("invalid_asset_size");
  }
  if (!digest.test(manifest.serviceWorker.sha256)) problems.push("service_worker_digest_mismatch");
  if (installedServiceWorkerVersion !== undefined && compareVersions(manifest.serviceWorker.version, installedServiceWorkerVersion) < 0) {
    problems.push("service_worker_downgrade");
  }
  if (!manifest.csp.nonceRequired || !manifest.csp.trustedTypesPolicy || !/^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/.test(manifest.csp.trustedTypesPolicy)) {
    problems.push("weak_csp");
  }
  return problems.length === 0 ? { ok: true } : { ok: false, problems: [...new Set(problems)] };
}

const compareVersions = (left: string, right: string): number => {
  const a = left.split(".").map((part) => Number(part));
  const b = right.split(".").map((part) => Number(part));
  if (a.every(Number.isFinite) && b.every(Number.isFinite)) {
    for (let i = 0; i < Math.max(a.length, b.length); i++) {
      const difference = (a[i] ?? 0) - (b[i] ?? 0);
      if (difference !== 0) return difference;
    }
    return 0;
  }
  return left.localeCompare(right);
};

export function securityHeaders(manifest: ReleaseManifest): Readonly<Record<string, string>> {
  return {
    "Content-Security-Policy": `default-src 'self'; script-src 'self' 'nonce-{nonce}'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; require-trusted-types-for 'script'; trusted-types ${manifest.csp.trustedTypesPolicy}`,
    "X-Content-Type-Options": "nosniff",
    "Referrer-Policy": "no-referrer",
    "Cache-Control": "no-store",
    "X-Client-Release": manifest.releaseId,
  };
}

export type ProtectedReference = { key: string; value: string; tenantId: string; expiresAt: number };
const allowedKey = /^(?:hcm-next:client-ref:|hcm-next:ui-preference:)/;
const sensitiveName = /(?:token|secret|password|cookie|salary|medical|bank|ssn|tax|case|document)/i;

export function isSafeProtectedReference(reference: ProtectedReference, now = Date.now()): boolean {
  return allowedKey.test(reference.key) && reference.value.length <= 256 && reference.tenantId.length > 0 && reference.expiresAt > now && !sensitiveName.test(reference.key);
}

export type ActionEnvelope = { actionId: string; schemaVersion: string; tenantId: string; issuedAt: number; payload: unknown };
export type ActionRevalidator = (action: ActionEnvelope) => Promise<boolean>;

export async function revalidateAction(action: ActionEnvelope, expectedSchemaVersion: string, expectedTenantId: string, verify: ActionRevalidator): Promise<boolean> {
  if (!action.actionId || action.schemaVersion !== expectedSchemaVersion || action.tenantId !== expectedTenantId) return false;
  return verify(action);
}

export type CacheState = { releaseId: string; schemaVersion: string; entries: Readonly<Record<string, unknown>> };

/** Keeps the active cache unchanged unless the complete candidate is accepted. */
export class AtomicCacheLifecycle {
  private active: CacheState | undefined;
  private activeServiceWorkerVersion: string | undefined;
  get snapshot(): CacheState | undefined { return this.active; }
  stage(candidate: CacheState, manifest: ReleaseManifest, trustedKeyIds: ReadonlySet<string>): boolean {
    const result = validateReleaseManifest(manifest, trustedKeyIds, this.activeServiceWorkerVersion);
    if (!result.ok || candidate.releaseId !== manifest.releaseId || candidate.schemaVersion !== manifest.schemaVersion) return false;
    this.active = { releaseId: candidate.releaseId, schemaVersion: candidate.schemaVersion, entries: { ...candidate.entries } };
    this.activeServiceWorkerVersion = manifest.serviceWorker.version;
    return true;
  }
  clear(): void { this.active = undefined; this.activeServiceWorkerVersion = undefined; }
}

export type StorageLike = { length: number; key(index: number): string | null; getItem(key: string): string | null; removeItem(key: string): void };

/** Clears all client state on logout/tenant switch; callers must provide every storage scope. */
export function clearClientState(storages: readonly StorageLike[]): void {
  for (const storage of storages) {
    const keys: string[] = [];
    for (let i = 0; i < storage.length; i++) { const key = storage.key(i); if (key !== null) keys.push(key); }
    for (const key of keys) storage.removeItem(key);
  }
}
