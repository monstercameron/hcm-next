import { describe, expect, it, vi } from "vitest";
import {
  DeviceStateStore,
  MemoryProtectedStorage,
  createProcessCodec,
  type DevicePosture,
  type OfflineDraft,
} from "./device-state";

const posture = (overrides: Partial<DevicePosture> = {}): DevicePosture => ({
  deviceId: "device-a",
  sessionId: "session-a",
  channel: "mobile",
  assurance: "trusted",
  attestedAt: Date.now(),
  ...overrides,
});

const input = (overrides: Partial<OfflineDraft> = {}) => ({
  draftId: "draft-1",
  tenantId: "tenant-1",
  proposalId: "proposal-1",
  payload: { secret: "clipboard-and-screenshot-sensitive" },
  risk: "write" as const,
  requestId: "request-1",
  deviceId: "device-a",
  sessionId: "session-a",
  ...overrides,
});

describe("CLIENT-002 security, recovery, model, and race matrix", () => {
  it("rejects rooted device posture before persisting a draft", () => {
    const storage = new MemoryProtectedStorage();
    const store = new DeviceStateStore(storage, createProcessCodec(), () => 10_000);

    expect(() => store.saveDraft(input(), posture({ assurance: "rooted" }))).toThrow(
      "assurance_required",
    );
    expect(storage.keys()).toEqual([]);
  });

  it("removes kiosk residue on handoff and keeps sensitive data out of storage text", () => {
    const storage = new MemoryProtectedStorage();
    const store = new DeviceStateStore(storage, createProcessCodec(), () => 10_000);
    store.saveDraft(input(), posture({ channel: "kiosk" }));

    const persisted = storage.get("hcm-next:device-state");
    expect(persisted).toBeDefined();
    expect(persisted).not.toContain("clipboard-and-screenshot-sensitive");
    store.handoff();
    expect(storage.keys()).toEqual([]);
    expect(store.loadDraft()).toBeUndefined();
  });

  it("treats an expired offline draft as stale authority and does not execute it", async () => {
    let now = 10_000;
    const store = new DeviceStateStore(
      new MemoryProtectedStorage(),
      createProcessCodec(),
      () => now,
    );
    const draft = store.saveDraft({ ...input(), ttlMs: 5 }, posture({ attestedAt: 1 }));
    now = draft.expiresAt + 1;
    const execute = vi.fn(async () => "must-not-run");

    expect(
      await store.resubmit(draft, posture(), true, async () => true, execute),
    ).toEqual({ ok: false, error: "expired" });
    expect(execute).not.toHaveBeenCalled();
  });

  it("replays an identical request but rejects a conflicting request digest", async () => {
    const store = new DeviceStateStore();
    const draft = store.saveDraft(input(), posture());
    const execute = vi.fn(async () => "durable");

    await expect(
      store.resubmit(draft, posture(), true, async () => true, execute),
    ).resolves.toMatchObject({ ok: true, replayed: false });
    await expect(
      store.resubmit(draft, posture(), true, async () => true, execute),
    ).resolves.toMatchObject({ ok: true, replayed: true });
    await expect(
      store.resubmit(
        { ...draft, payload: { changed: true } },
        posture(),
        true,
        async () => true,
        execute,
      ),
    ).resolves.toEqual({ ok: false, error: "idempotency_conflict" });
    expect(execute).toHaveBeenCalledTimes(1);
  });

  it("serializes concurrent replay attempts so one durable effect is produced", async () => {
    const store = new DeviceStateStore();
    const draft = store.saveDraft(input(), posture());
    const execute = vi.fn(async () => "durable");
    const authorize = async () => true;

    const outcomes = await Promise.all([
      store.resubmit(draft, posture(), true, authorize, execute),
      store.resubmit(draft, posture(), true, authorize, execute),
    ]);

    expect(outcomes.filter((result) => result.ok && !result.replayed)).toHaveLength(1);
    expect(outcomes.filter((result) => result.ok && result.replayed)).toHaveLength(1);
    expect(execute).toHaveBeenCalledTimes(1);
  });
});
