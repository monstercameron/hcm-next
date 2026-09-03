import { describe, expect, it, vi } from "vitest";
import { AtomicCacheLifecycle, clearClientState, isSafeProtectedReference, revalidateAction, securityHeaders, validateReleaseManifest, type ReleaseManifest, type StorageLike } from "./lifecycle";

const manifest = (overrides: Partial<ReleaseManifest> = {}): ReleaseManifest => ({
  releaseId: "r2", schemaVersion: "s2", keyId: "release-key", signature: "signed",
  assets: [{ path: "/app.js", sha256: "a".repeat(64), size: 10 }],
  serviceWorker: { version: "2", sha256: "b".repeat(64) },
  csp: { nonceRequired: true, trustedTypesPolicy: "hcmClient" }, ...overrides,
});

describe("CLIENT-001 browser lifecycle", () => {
  it("accepts pinned signed releases and emits strict headers", () => {
    expect(validateReleaseManifest(manifest(), new Set(["release-key"]))).toEqual({ ok: true });
    expect(securityHeaders(manifest())["Content-Security-Policy"]).toContain("require-trusted-types-for");
  });
  it("rejects unsigned, weak, duplicate and downgraded releases", () => {
    const bad = manifest({ signature: "", csp: { nonceRequired: false, trustedTypesPolicy: "" }, serviceWorker: { version: "1", sha256: "x" }, assets: [{ path: "/app.js", sha256: "x", size: -1 }, { path: "/app.js", sha256: "x", size: 1 }] });
    const result = validateReleaseManifest(bad, new Set(["release-key"]), "2");
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.problems).toEqual(expect.arrayContaining(["unsigned_release", "weak_csp", "service_worker_downgrade", "duplicate_asset", "invalid_asset_digest"]));
  });
  it("upgrades cache atomically and refuses stale candidates", () => {
    const cache = new AtomicCacheLifecycle();
    expect(cache.stage({ releaseId: "r2", schemaVersion: "s2", entries: { ok: 1 } }, manifest(), new Set(["release-key"]))).toBe(true);
    expect(cache.stage({ releaseId: "r1", schemaVersion: "s1", entries: { bad: 1 } }, manifest({ releaseId: "r1", schemaVersion: "s1", serviceWorker: { version: "1", sha256: "b".repeat(64) } }), new Set(["release-key"]))).toBe(false);
    expect(cache.snapshot?.entries).toEqual({ ok: 1 });
  });
  it("revalidates actions against current tenant and schema", async () => {
    const verify = vi.fn(async () => true);
    expect(await revalidateAction({ actionId: "a", schemaVersion: "s2", tenantId: "t1", issuedAt: 0, payload: {} }, "s2", "t1", verify)).toBe(true);
    expect(await revalidateAction({ actionId: "a", schemaVersion: "s1", tenantId: "t1", issuedAt: 0, payload: {} }, "s2", "t1", verify)).toBe(false);
    expect(verify).toHaveBeenCalledTimes(1);
  });
  it("only permits bounded reviewed references and clears every storage scope", () => {
    expect(isSafeProtectedReference({ key: "hcm-next:client-ref:release", value: "r2", tenantId: "t1", expiresAt: Date.now() + 1000 })).toBe(true);
    expect(isSafeProtectedReference({ key: "hcm-next:client-ref:token", value: "secret", tenantId: "t1", expiresAt: Date.now() + 1000 })).toBe(false);
    const values = new Map([["a", "1"], ["b", "2"]]);
    const storage: StorageLike = { get length() { return values.size; }, key: i => [...values.keys()][i] ?? null, getItem: k => values.get(k) ?? null, removeItem: k => values.delete(k) };
    clearClientState([storage]); expect(values.size).toBe(0);
  });
});
