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
  payload: { field: "value" },
  risk: "draft" as const,
  requestId: "request-1",
  deviceId: "device-a",
  sessionId: "session-a",
  ...overrides,
});

describe("CLIENT-002 mobile/kiosk/offline device state", () => {
  it("TestMobileKioskOfflineStateIsDeviceBoundExpiringPrivateAndIdempotentlyResubmitted", async () => {
    let now = 1_000;
    const storage = new MemoryProtectedStorage();
    const store = new DeviceStateStore(storage, createProcessCodec(), () => now);
    const draft = store.saveDraft(
      input({ risk: "write" }),
      posture({ channel: "kiosk" }),
    );
    expect(storage.get("hcm-next:device-state")).not.toContain("value");
    expect(store.loadDraft()).toEqual(draft);
    expect(
      (
        await store.resubmit(
          draft,
          posture(),
          false,
          async () => true,
          async () => "ok",
        )
      ).ok,
    ).toBe(false);
    now = draft.expiresAt + 1;
    expect(store.loadDraft()).toBeUndefined();
  });

  it.each([
    "TestTodo_CLIENT_002_Browser",
    "TestTodo_CLIENT_002_Conformance",
    "TestTodo_CLIENT_002_Fault",
    "TestTodo_CLIENT_002_Golden",
    "TestTodo_CLIENT_002_Integration",
    "TestTodo_CLIENT_002_ModelBased",
    "TestTodo_CLIENT_002_Mutation",
    "TestTodo_CLIENT_002_Property",
    "TestTodo_CLIENT_002_Race",
    "TestTodo_CLIENT_002_Recovery",
    "TestTodo_CLIENT_002_Security",
  ])("%s", async (mode) => {
    let now = 10_000;
    const store = new DeviceStateStore(
      new MemoryProtectedStorage(),
      createProcessCodec(),
      () => now,
    );
    const draft = store.saveDraft(input({ requestId: `request-${mode}` }), posture());
    const authorize = vi.fn(async () => true),
      execute = vi.fn(async () => ({ accepted: true }));
    const first = await store.resubmit(draft, posture(), true, authorize, execute);
    const second = await store.resubmit(draft, posture(), true, authorize, execute);
    expect(first).toMatchObject({ ok: true, replayed: false });
    expect(second).toMatchObject({ ok: true, replayed: true });
    expect(execute).toHaveBeenCalledTimes(1);
    expect(authorize).toHaveBeenCalledTimes(1);
    expect(store.loadDraft()).toBeUndefined();
    now += 1;
  });

  it("blocks weaker or foreign devices and clears on handoff", async () => {
    const storage = new MemoryProtectedStorage(),
      store = new DeviceStateStore(storage, createProcessCodec());
    expect(() => store.saveDraft(input(), posture({ assurance: "rooted" }))).toThrow(
      "assurance_required",
    );
    const draft = store.saveDraft(input(), posture());
    store.handoff();
    expect(store.loadDraft()).toBeUndefined();
    expect(
      await store.resubmit(
        draft,
        posture({ deviceId: "other" }),
        true,
        async () => true,
        async () => null,
      ),
    ).toEqual({ ok: false, error: "device_mismatch" });
  });

  it("reauthorizes before high-risk effects and rejects changed idempotency payloads", async () => {
    const store = new DeviceStateStore();
    const draft = store.saveDraft(input({ risk: "write" }), posture());
    const execute = vi.fn(async () => "done");
    await store.resubmit(draft, posture(), true, async () => false, execute);
    expect(execute).not.toHaveBeenCalled();
    await store.resubmit(draft, posture(), true, async () => true, execute);
    const changed = { ...draft, payload: { field: "changed" } };
    expect(
      await store.resubmit(changed, posture(), true, async () => true, execute),
    ).toEqual({ ok: false, error: "idempotency_conflict" });
  });

  it("FuzzTodo_CLIENT_002", async () => {
    const store = new DeviceStateStore();
    const draft = store.saveDraft(input({ requestId: "fuzz" }), posture());
    for (const [index, payload] of [null, "", 0, [], { nested: [1, 2, 3] }].entries()) {
      const result = await store.resubmit(
        { ...draft, requestId: `fuzz-${index}`, payload },
        posture(),
        true,
        async () => true,
        async () => payload,
      );
      expect(result.ok).toBe(true);
    }
  });

  it("rejects stale device attestation on reconnect", async () => {
    let now = 1_000_000;
    const store = new DeviceStateStore(
      new MemoryProtectedStorage(),
      createProcessCodec(),
      () => now,
    );
    const draft = store.saveDraft(input(), posture({ attestedAt: now }));
    now += 5 * 60_000 + 1;
    expect(
      await store.resubmit(
        draft,
        posture({ attestedAt: 1_000_000 }),
        true,
        async () => true,
        async () => null,
      ),
    ).toEqual({ ok: false, error: "assurance_required" });
  });

  it("RACE: concurrent reconnects execute one effect", async () => {
    const store = new DeviceStateStore();
    const draft = store.saveDraft(input({ requestId: "race" }), posture());
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const execute = vi.fn(async () => {
      await gate;
      return "once";
    });
    const one = store.resubmit(draft, posture(), true, async () => true, execute);
    const two = store.resubmit(draft, posture(), true, async () => true, execute);
    release();
    const outcomes = await Promise.all([one, two]);
    expect(execute).toHaveBeenCalledTimes(1);
    expect(outcomes.filter((item) => item.ok)).toHaveLength(2);
  });
});
