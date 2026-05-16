import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PageDefinition } from "@hcm-next/ui-contracts";
import {
  clearCachedPages,
  getCachedPage,
  listCachedEntries,
  setCachedPage,
  signatureForKey,
  type UiCacheKey,
} from "./ui-cache.js";

/**
 * Minimal in-memory `Storage` polyfill so tests can run under the default
 * Node vitest environment without pulling in jsdom/happy-dom. The class
 * intentionally only implements what the cache touches.
 */
class MemoryStorage implements Storage {
  private readonly store = new Map<string, string>();
  private shouldThrowOnSet = false;

  failNextWrites(): void {
    this.shouldThrowOnSet = true;
  }

  get length(): number {
    return this.store.size;
  }

  clear(): void {
    this.store.clear();
  }

  getItem(key: string): string | null {
    return this.store.has(key) ? (this.store.get(key) ?? null) : null;
  }

  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null;
  }

  removeItem(key: string): void {
    this.store.delete(key);
  }

  setItem(key: string, value: string): void {
    if (this.shouldThrowOnSet) {
      throw new DOMException("QuotaExceededError", "QuotaExceededError");
    }
    this.store.set(key, value);
  }
}

const storageKey = "hcm-next:ai-ui-cache:v1";

const buildPage = (id: string): PageDefinition => ({
  id,
  title: `Page ${id}`,
  description: "Generated for tests.",
  workflowTypes: ["employee.termination"],
  surfaceModes: ["full_app"],
  regions: [
    {
      id: "main",
      layout: "stack",
      width: "content",
      widgets: [],
    },
  ],
});

const baseKey: UiCacheKey = {
  workflowConfigHash: "hash-a",
  currentState: "draft",
  actorRole: "hr_partner",
  refinementSequence: [],
};

let memoryStorage: MemoryStorage;

beforeEach(() => {
  memoryStorage = new MemoryStorage();
  // Establish a browser-shaped global so the cache treats this environment
  // as a real client. `vi.stubGlobal` automatically reverts on `unstubAllGlobals`.
  vi.stubGlobal("window", { localStorage: memoryStorage });
  vi.stubGlobal("localStorage", memoryStorage);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("signatureForKey", () => {
  it("produces a stable hex digest for the same inputs across calls", async () => {
    const first = await signatureForKey(baseKey);
    const second = await signatureForKey({
      ...baseKey,
      refinementSequence: [...baseKey.refinementSequence],
    });
    expect(first).toBe(second);
    expect(first).toMatch(/^[0-9a-f]{40}$/);
  });

  it("yields a different signature when currentState differs", async () => {
    const draft = await signatureForKey({ ...baseKey, currentState: "draft" });
    const review = await signatureForKey({ ...baseKey, currentState: "review" });
    expect(draft).not.toBe(review);
  });

  it("yields a different signature when workflowConfigHash differs", async () => {
    const hashA = await signatureForKey({ ...baseKey, workflowConfigHash: "hash-a" });
    const hashB = await signatureForKey({ ...baseKey, workflowConfigHash: "hash-b" });
    expect(hashA).not.toBe(hashB);
  });

  it("treats refinementSequence as order-significant", async () => {
    const forward = await signatureForKey({
      ...baseKey,
      refinementSequence: ["one", "two"],
    });
    const reversed = await signatureForKey({
      ...baseKey,
      refinementSequence: ["two", "one"],
    });
    expect(forward).not.toBe(reversed);
  });
});

describe("setCachedPage / getCachedPage", () => {
  it("round-trips a page through the cache", async () => {
    const page = buildPage("draft-page");
    await setCachedPage(baseKey, page);
    const fetched = await getCachedPage(baseKey);
    expect(fetched).toEqual(page);
  });

  it("returns undefined when the key is absent", async () => {
    const fetched = await getCachedPage(baseKey);
    expect(fetched).toBeUndefined();
  });

  it("returns undefined when the stored JSON is corrupt and does not throw", async () => {
    memoryStorage.setItem(storageKey, "{not valid json");
    const fetched = await getCachedPage(baseKey);
    expect(fetched).toBeUndefined();
    // Corrupt blob should be evicted so future writes succeed cleanly.
    expect(memoryStorage.getItem(storageKey)).toBeNull();
  });

  it("returns undefined when the parsed JSON is not a cache record", async () => {
    memoryStorage.setItem(storageKey, JSON.stringify(["not", "a", "record"]));
    const fetched = await getCachedPage(baseKey);
    expect(fetched).toBeUndefined();
  });
});

describe("clearCachedPages", () => {
  it("removes every entry", async () => {
    await setCachedPage(baseKey, buildPage("p1"));
    await setCachedPage({ ...baseKey, currentState: "review" }, buildPage("p2"));
    expect(listCachedEntries()).toHaveLength(2);

    clearCachedPages();

    expect(listCachedEntries()).toHaveLength(0);
    expect(await getCachedPage(baseKey)).toBeUndefined();
  });
});

describe("cap enforcement", () => {
  it("evicts the oldest entry (by storedAt) when exceeding the 50-entry cap", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00.000Z"));

    const oldestKey: UiCacheKey = {
      ...baseKey,
      currentState: "state-oldest",
    };
    await setCachedPage(oldestKey, buildPage("oldest"));

    // Fill 49 more entries each one second after the previous so storedAt
    // is monotonically increasing.
    for (let index = 0; index < 49; index += 1) {
      vi.setSystemTime(new Date(Date.UTC(2026, 0, 1, 0, 0, index + 1)));
      await setCachedPage(
        { ...baseKey, currentState: `state-${index}` },
        buildPage(`page-${index}`),
      );
    }

    // Cache is now at the cap.
    expect(listCachedEntries()).toHaveLength(50);
    expect(await getCachedPage(oldestKey)).toBeDefined();

    // One more entry trips the cap; the oldest must be evicted.
    vi.setSystemTime(new Date("2026-01-01T01:00:00.000Z"));
    await setCachedPage(
      { ...baseKey, currentState: "state-newest" },
      buildPage("newest"),
    );

    expect(listCachedEntries()).toHaveLength(50);
    expect(await getCachedPage(oldestKey)).toBeUndefined();
    expect(
      await getCachedPage({ ...baseKey, currentState: "state-newest" }),
    ).toBeDefined();
  });
});

describe("storage failures", () => {
  it("silently skips caching when localStorage throws on write (quota)", async () => {
    memoryStorage.failNextWrites();
    // Must not throw despite the underlying boundary rejecting the write.
    await expect(setCachedPage(baseKey, buildPage("ignored"))).resolves.toBeUndefined();
    expect(memoryStorage.getItem(storageKey)).toBeNull();
  });

  it("returns undefined from getCachedPage when window is absent (SSR)", async () => {
    vi.unstubAllGlobals();
    const fetched = await getCachedPage(baseKey);
    expect(fetched).toBeUndefined();
  });

  it("no-ops setCachedPage when window is absent (SSR)", async () => {
    vi.unstubAllGlobals();
    await expect(setCachedPage(baseKey, buildPage("ssr"))).resolves.toBeUndefined();
  });
});
