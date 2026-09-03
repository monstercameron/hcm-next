import { describe, expect, it, vi } from "vitest";
import {
  AtomicCacheLifecycle,
  clearClientState,
  isSafeProtectedReference,
  revalidateAction,
  securityHeaders,
  validateReleaseManifest,
  type ReleaseManifest,
  type StorageLike,
} from "./lifecycle";

const manifest = (overrides: Partial<ReleaseManifest> = {}): ReleaseManifest => ({
  releaseId: "r2",
  schemaVersion: "s2",
  keyId: "release-key",
  signature: "signed",
  assets: [{ path: "/app.js", sha256: "a".repeat(64), size: 10 }],
  serviceWorker: { version: "2", sha256: "b".repeat(64) },
  csp: { nonceRequired: true, trustedTypesPolicy: "hcmClient" },
  ...overrides,
});

describe("CLIENT-001 browser lifecycle", () => {
  it("TestBrowserArtifactPolicyCacheAndStorageLifecycleRejectsStaleInjectedOrSensitiveState", async () => {
    const cache = new AtomicCacheLifecycle();
    expect(
      cache.stage(
        { releaseId: "r2", schemaVersion: "s2", entries: { safe: true } },
        manifest(),
        new Set(["release-key"]),
      ),
    ).toBe(true);
    expect(
      cache.stage(
        { releaseId: "r3", schemaVersion: "s2", entries: { injected: true } },
        manifest({
          assets: [{ path: "/../injected.js", sha256: "a".repeat(64), size: 1 }],
        }),
        new Set(["release-key"]),
      ),
    ).toBe(false);
    expect(cache.snapshot?.entries).toEqual({ safe: true });
    expect(
      isSafeProtectedReference({
        key: "hcm-next:client-ref:",
        value: "x",
        tenantId: "t1",
        expiresAt: Date.now() + 1000,
      }),
    ).toBe(false);
    expect(
      isSafeProtectedReference({
        key: "hcm-next:client-ref:release",
        value: "salary=100000",
        tenantId: "t1",
        expiresAt: Date.now() + 1000,
      }),
    ).toBe(false);
    expect(
      await revalidateAction(
        {
          actionId: "stale",
          schemaVersion: "s2",
          tenantId: "t1",
          issuedAt: 0,
          expiresAt: Date.now() - 1,
          payload: {},
        },
        "s2",
        "t1",
        vi.fn(async () => true),
      ),
    ).toBe(false);
  });

  it("accepts pinned signed releases and emits strict headers", () => {
    expect(validateReleaseManifest(manifest(), new Set(["release-key"]))).toEqual({
      ok: true,
    });
    expect(securityHeaders(manifest())["Content-Security-Policy"]).toContain(
      "require-trusted-types-for",
    );
  });
  it("rejects unsigned, weak, duplicate and downgraded releases", () => {
    const bad = manifest({
      signature: "",
      csp: { nonceRequired: false, trustedTypesPolicy: "" },
      serviceWorker: { version: "1", sha256: "x" },
      assets: [
        { path: "/app.js", sha256: "x", size: -1 },
        { path: "/app.js", sha256: "x", size: 1 },
      ],
    });
    const result = validateReleaseManifest(bad, new Set(["release-key"]), "2");
    expect(result.ok).toBe(false);
    if (!result.ok)
      expect(result.problems).toEqual(
        expect.arrayContaining([
          "unsigned_release",
          "weak_csp",
          "service_worker_downgrade",
          "duplicate_asset",
          "invalid_asset_digest",
        ]),
      );
  });
  it("rejects untrusted keys, malformed asset paths, and invalid worker versions", () => {
    const result = validateReleaseManifest(
      manifest({
        keyId: "other",
        assets: [{ path: "/../app.js", sha256: "a".repeat(64), size: 1 }],
        serviceWorker: { version: "latest", sha256: "b".repeat(64) },
      }),
      new Set(["release-key"]),
    );
    expect(result).toEqual({
      ok: false,
      problems: expect.arrayContaining([
        "untrusted_key",
        "invalid_asset_path",
        "invalid_service_worker_version",
      ]),
    });
  });
  it("upgrades cache atomically and refuses stale candidates", () => {
    const cache = new AtomicCacheLifecycle();
    expect(
      cache.stage(
        { releaseId: "r2", schemaVersion: "s2", entries: { ok: 1 } },
        manifest(),
        new Set(["release-key"]),
      ),
    ).toBe(true);
    expect(
      cache.stage(
        { releaseId: "r1", schemaVersion: "s1", entries: { bad: 1 } },
        manifest({
          releaseId: "r1",
          schemaVersion: "s1",
          serviceWorker: { version: "1", sha256: "b".repeat(64) },
        }),
        new Set(["release-key"]),
      ),
    ).toBe(false);
    expect(cache.snapshot?.entries).toEqual({ ok: 1 });
  });
  it("revalidates actions against current tenant and schema", async () => {
    const verify = vi.fn(async () => true);
    expect(
      await revalidateAction(
        {
          actionId: "a",
          schemaVersion: "s2",
          tenantId: "t1",
          issuedAt: 0,
          payload: {},
        },
        "s2",
        "t1",
        verify,
      ),
    ).toBe(true);
    expect(
      await revalidateAction(
        {
          actionId: "a",
          schemaVersion: "s1",
          tenantId: "t1",
          issuedAt: 0,
          payload: {},
        },
        "s2",
        "t1",
        verify,
      ),
    ).toBe(false);
    expect(verify).toHaveBeenCalledTimes(1);
  });
  it("only permits bounded reviewed references and clears every storage scope", () => {
    expect(
      isSafeProtectedReference({
        key: "hcm-next:client-ref:release",
        value: "r2",
        tenantId: "t1",
        expiresAt: Date.now() + 1000,
      }),
    ).toBe(true);
    expect(
      isSafeProtectedReference({
        key: "hcm-next:client-ref:token",
        value: "secret",
        tenantId: "t1",
        expiresAt: Date.now() + 1000,
      }),
    ).toBe(false);
    const values = new Map([
      ["a", "1"],
      ["b", "2"],
    ]);
    const storage: StorageLike = {
      get length() {
        return values.size;
      },
      key: (i) => [...values.keys()][i] ?? null,
      getItem: (k) => values.get(k) ?? null,
      removeItem: (k) => values.delete(k),
    };
    clearClientState([storage]);
    expect(values.size).toBe(0);
  });

  it("TestTodo_CLIENT_001_Browser", () => {
    const headers = securityHeaders(manifest());
    expect(headers["X-Content-Type-Options"]).toBe("nosniff");
    expect(headers["Referrer-Policy"]).toBe("no-referrer");
    expect(headers["Cache-Control"]).toBe("no-store");
  });

  it("TestTodo_CLIENT_001_Conformance", () => {
    const cache = new AtomicCacheLifecycle();
    expect(
      cache.stage(
        { releaseId: "r2", schemaVersion: "s2", entries: {} },
        manifest(),
        new Set(["release-key"]),
      ),
    ).toBe(true);
    expect(
      cache.stage(
        { releaseId: "r3", schemaVersion: "wrong", entries: {} },
        manifest({ releaseId: "r3" }),
        new Set(["release-key"]),
      ),
    ).toBe(false);
  });

  it("TestTodo_CLIENT_001_Fault", async () => {
    const verify = vi.fn(async () => false);
    expect(
      await revalidateAction(
        {
          actionId: "a",
          schemaVersion: "s2",
          tenantId: "t1",
          issuedAt: 0,
          payload: {},
        },
        "s2",
        "t1",
        verify,
      ),
    ).toBe(false);
    expect(verify).toHaveBeenCalledOnce();
  });

  it("FuzzTodo_CLIENT_001", () => {
    for (const path of ["", "app.js", "/../x", "/x\\n.js", "/x.js?script=1"]) {
      const result = validateReleaseManifest(
        manifest({ assets: [{ path, sha256: "a".repeat(64), size: 1 }] }),
        new Set(["release-key"]),
      );
      expect(result.ok).toBe(path === "/app.js");
    }
  });

  it("TestTodo_CLIENT_001_Golden", () => {
    expect(securityHeaders(manifest())).toMatchObject({
      "Content-Security-Policy": expect.stringContaining("default-src 'self'"),
      "X-Client-Release": "r2",
    });
  });

  it("TestTodo_CLIENT_001_Integration", () => {
    const cache = new AtomicCacheLifecycle();
    expect(
      cache.stage(
        { releaseId: "r2", schemaVersion: "s2", entries: { draft: "reference" } },
        manifest(),
        new Set(["release-key"]),
      ),
    ).toBe(true);
    cache.clear();
    expect(cache.snapshot).toBeUndefined();
  });

  it("TestTodo_CLIENT_001_Mutation", () => {
    const candidate = {
      releaseId: "r2",
      schemaVersion: "s2",
      entries: { state: "safe" },
    };
    const cache = new AtomicCacheLifecycle();
    expect(cache.stage(candidate, manifest(), new Set(["release-key"]))).toBe(true);
    candidate.entries.state = "mutated";
    expect(cache.snapshot?.entries).toEqual({ state: "safe" });
  });

  it("TestTodo_CLIENT_001_Property", () => {
    for (const installed of [undefined, "1", "2", "3"]) {
      const result = validateReleaseManifest(
        manifest(),
        new Set(["release-key"]),
        installed,
      );
      expect(result.ok).toBe(installed !== "3");
    }
  });

  it("TestTodo_CLIENT_001_Recovery", () => {
    const cache = new AtomicCacheLifecycle();
    expect(
      cache.stage(
        { releaseId: "r2", schemaVersion: "s2", entries: {} },
        manifest(),
        new Set(["release-key"]),
      ),
    ).toBe(true);
    cache.clear();
    expect(
      cache.stage(
        { releaseId: "r2", schemaVersion: "s2", entries: { recovered: true } },
        manifest(),
        new Set(["release-key"]),
      ),
    ).toBe(true);
    expect(cache.snapshot?.entries).toEqual({ recovered: true });
  });

  it("TestTodo_CLIENT_001_Security", async () => {
    expect(
      isSafeProtectedReference({
        key: "hcm-next:ui-preference:theme",
        value: "dark",
        tenantId: "t1",
        expiresAt: Date.now() + 1000,
      }),
    ).toBe(true);
    expect(
      isSafeProtectedReference({
        key: "hcm-next:ui-preference:theme",
        value: "dark\nSet-Cookie",
        tenantId: "t1",
        expiresAt: Date.now() + 1000,
      }),
    ).toBe(false);
    expect(
      await revalidateAction(
        {
          actionId: "a",
          schemaVersion: "s2",
          tenantId: "t2",
          issuedAt: 0,
          payload: {},
        },
        "s2",
        "t1",
        vi.fn(async () => true),
      ),
    ).toBe(false);
  });
});
