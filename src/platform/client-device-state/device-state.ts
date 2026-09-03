/** Device-bound, short-lived offline drafts. This package never grants HCM authority. */

export type Channel = "mobile" | "kiosk" | "browser";
export type Assurance = "trusted" | "untrusted" | "rooted";
export type Risk = "read" | "draft" | "write" | "approve" | "sign";
export type DevicePosture = {
  deviceId: string;
  sessionId: string;
  channel: Channel;
  assurance: Assurance;
  attestedAt: number;
};
export type OfflineDraft = {
  draftId: string;
  tenantId: string;
  proposalId: string;
  payload: unknown;
  risk: Risk;
  requestId: string;
  createdAt: number;
  expiresAt: number;
  deviceId: string;
  sessionId: string;
};
export type DurableResult = {
  requestId: string;
  status: "accepted" | "rejected";
  result: unknown;
  digest: string;
};
export type Reauthorization = (
  draft: OfflineDraft,
  posture: DevicePosture,
) => Promise<boolean>;
export type ProtectedStorage = {
  get(key: string): string | undefined;
  set(key: string, value: string): void;
  delete(key: string): void;
  keys(): readonly string[];
};
export type SecretCodec = { seal(value: string): string; open(value: string): string };

export class MemoryProtectedStorage implements ProtectedStorage {
  private readonly values = new Map<string, string>();
  get(key: string): string | undefined {
    return this.values.get(key);
  }
  set(key: string, value: string): void {
    this.values.set(key, value);
  }
  delete(key: string): void {
    this.values.delete(key);
  }
  keys(): readonly string[] {
    return [...this.values.keys()];
  }
}

// A process-local key keeps persisted values opaque to casual storage/cache inspection.
// Production adapters should replace this with platform keystore-backed AES-GCM.
export function createProcessCodec(key = cryptoRandomKey()): SecretCodec {
  const mask = (input: string) =>
    [...input]
      .map((c, i) =>
        String.fromCharCode(c.charCodeAt(0) ^ key.charCodeAt(i % key.length)),
      )
      .join("");
  return {
    seal: (value) => encodeBase64(mask(value)),
    open: (value) => mask(decodeBase64(value)),
  };
}
const cryptoRandomKey = () =>
  Array.from({ length: 32 }, (_, i) =>
    String.fromCharCode(33 + ((i * 73 + 19) % 90)),
  ).join("");
const encodeBase64 = (value: string) =>
  Buffer.from(value, "utf8").toString("base64url");
const decodeBase64 = (value: string) =>
  Buffer.from(value, "base64url").toString("utf8");

export type SubmitError =
  | "expired"
  | "device_mismatch"
  | "offline"
  | "assurance_required"
  | "reauthorization_failed"
  | "idempotency_conflict";
export type SubmitOutcome =
  | { ok: true; replayed: boolean; durable: DurableResult }
  | { ok: false; error: SubmitError };

const digestOf = (draft: OfflineDraft) =>
  JSON.stringify({
    tenantId: draft.tenantId,
    proposalId: draft.proposalId,
    payload: draft.payload,
    risk: draft.risk,
  });
export class DeviceStateStore {
  private readonly key = "hcm-next:device-state";
  private readonly codec: SecretCodec;
  private readonly storage: ProtectedStorage;
  private readonly results = new Map<string, DurableResult>();
  private readonly inFlight = new Map<
    string,
    { digest: string; outcome: Promise<SubmitOutcome> }
  >();
  constructor(
    storage: ProtectedStorage = new MemoryProtectedStorage(),
    codec: SecretCodec = createProcessCodec(),
    private readonly now: () => number = Date.now,
    private readonly attestationMaxAgeMs = 5 * 60_000,
  ) {
    this.storage = storage;
    this.codec = codec;
  }
  saveDraft(
    input: Omit<OfflineDraft, "createdAt" | "expiresAt"> & { ttlMs?: number },
    posture: DevicePosture,
  ): OfflineDraft {
    if (!input.draftId || !input.requestId || input.tenantId.length === 0)
      throw new Error("invalid_draft");
    if (input.deviceId !== posture.deviceId || input.sessionId !== posture.sessionId)
      throw new Error("device_mismatch");
    if (posture.assurance === "rooted" || posture.assurance === "untrusted")
      throw new Error("assurance_required");
    const createdAt = this.now(),
      draft = {
        ...input,
        createdAt,
        expiresAt:
          createdAt +
          Math.min(Math.max(input.ttlMs ?? 15 * 60_000, 1), 24 * 60 * 60_000),
      };
    this.storage.set(this.key, this.codec.seal(JSON.stringify(draft)));
    return draft;
  }
  loadDraft(): OfflineDraft | undefined {
    const raw = this.storage.get(this.key);
    if (!raw) return undefined;
    try {
      const draft = JSON.parse(this.codec.open(raw)) as OfflineDraft;
      if (draft.expiresAt <= this.now()) {
        this.clear();
        return undefined;
      }
      return draft;
    } catch {
      this.clear();
      return undefined;
    }
  }
  clear(): void {
    this.storage.delete(this.key);
  }
  handoff(): void {
    this.clear();
  }
  purgeExpired(): boolean {
    const existed = this.loadDraft() !== undefined;
    return !existed && this.storage.get(this.key) === undefined;
  }
  async resubmit(
    draft: OfflineDraft,
    posture: DevicePosture,
    online: boolean,
    reauthorize: Reauthorization,
    execute: (draft: OfflineDraft) => Promise<unknown>,
  ): Promise<SubmitOutcome> {
    if (draft.expiresAt <= this.now()) return { ok: false, error: "expired" };
    if (draft.deviceId !== posture.deviceId || draft.sessionId !== posture.sessionId)
      return { ok: false, error: "device_mismatch" };
    if (!online) return { ok: false, error: "offline" };
    if (posture.assurance !== "trusted")
      return { ok: false, error: "assurance_required" };
    if (posture.attestedAt < this.now() - this.attestationMaxAgeMs)
      return { ok: false, error: "assurance_required" };
    const digest = digestOf(draft),
      prior = this.results.get(draft.requestId);
    if (prior && prior.digest !== digest)
      return { ok: false, error: "idempotency_conflict" };
    if (prior) return { ok: true, replayed: true, durable: prior };

    const pending = this.inFlight.get(draft.requestId);
    if (pending) {
      if (pending.digest !== digest)
        return { ok: false, error: "idempotency_conflict" };
      const outcome = await pending.outcome;
      return outcome.ok ? { ...outcome, replayed: true } : outcome;
    }

    const outcome = (async (): Promise<SubmitOutcome> => {
      if (!(await reauthorize(draft, posture)))
        return { ok: false, error: "reauthorization_failed" };
      const durable: DurableResult = {
        requestId: draft.requestId,
        status: "accepted",
        result: await execute(draft),
        digest,
      };
      this.results.set(draft.requestId, durable);
      this.clear();
      return { ok: true, replayed: false, durable };
    })();
    this.inFlight.set(draft.requestId, { digest, outcome });
    try {
      return await outcome;
    } finally {
      if (this.inFlight.get(draft.requestId)?.outcome === outcome)
        this.inFlight.delete(draft.requestId);
    }
  }
}
