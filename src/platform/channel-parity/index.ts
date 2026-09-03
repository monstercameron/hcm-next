import { createHash } from "node:crypto";

export const CHANNELS = [
  "desktop",
  "mobile",
  "kiosk",
  "accessibility-assisted",
] as const;
export type IntentCreationChannel = (typeof CHANNELS)[number];

export type Capability = { id: string; version: string };
export type TrustedContext = {
  tenantId: string;
  principalId: string;
  purpose: string;
  capability: Capability;
};

export type IntentCreationInput = {
  intentType: string;
  fields: Readonly<Record<string, unknown>>;
};

export type ChannelAdaptation = {
  inputMode: "pointer-keyboard" | "touch" | "guided-touch" | "screen-reader";
  confirmation: "standard" | "large-targets" | "guided" | "announced";
  simulation: "available";
  assistance?: {
    kind: "representative" | "interpreter" | "screen-reader";
    actorId: string;
  };
  handoff?: { kind: "safe-resume"; reference: string; authorityToken: false };
};

export type NormalizedIntent = {
  intentType: string;
  fields: Readonly<Record<string, unknown>>;
  capability: Capability;
  trustedContext: TrustedContext;
};

export type IntentCreationSuccess = {
  ok: true;
  channel: IntentCreationChannel;
  normalizedIntent: NormalizedIntent;
  requestDigest: string;
  result: { status: "created"; intentType: string; capability: Capability };
  adaptation: ChannelAdaptation;
  evidence: { confirmationRequired: true; simulationAvailable: true };
};

export type IntentCreationError = {
  ok: false;
  channel: IntentCreationChannel;
  error: {
    code: "INVALID_REQUIRED_INPUT" | "INVALID_CONTEXT";
    fields: readonly string[];
    message: string;
  };
  requestDigest: string;
  adaptation: ChannelAdaptation;
};

export type IntentCreationOutcome = IntentCreationSuccess | IntentCreationError;

const REQUIRED_FIELDS: Readonly<Record<string, readonly string[]>> = {
  "leave-request": ["startDate", "endDate", "reason"],
};

function canonical(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  const record = value as Record<string, unknown>;
  return `{${Object.keys(record)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${canonical(record[key])}`)
    .join(",")}}`;
}

function digest(value: unknown): string {
  return createHash("sha256").update(canonical(value), "utf8").digest("hex");
}

function adaptation(
  channel: IntentCreationChannel,
  assistance?: ChannelAdaptation["assistance"],
): ChannelAdaptation {
  switch (channel) {
    case "desktop":
      return {
        inputMode: "pointer-keyboard",
        confirmation: "standard",
        simulation: "available",
      };
    case "mobile":
      return {
        inputMode: "touch",
        confirmation: "large-targets",
        simulation: "available",
      };
    case "kiosk":
      return {
        inputMode: "guided-touch",
        confirmation: "guided",
        simulation: "available",
        handoff: {
          kind: "safe-resume",
          reference: "resume:opaque",
          authorityToken: false,
        },
      };
    case "accessibility-assisted":
      return {
        inputMode: "screen-reader",
        confirmation: "announced",
        simulation: "available",
        ...(assistance ? { assistance } : {}),
      };
  }
}

function requestDigest(input: IntentCreationInput, context: TrustedContext): string {
  return digest({
    intentType: input.intentType,
    fields: input.fields,
    capability: context.capability,
    tenantId: context.tenantId,
    purpose: context.purpose,
  });
}

/** One semantic creation path; channel adapters only affect presentation metadata. */
export function createIntent(
  channel: IntentCreationChannel,
  input: IntentCreationInput,
  context: TrustedContext,
  assistance?: ChannelAdaptation["assistance"],
): IntentCreationOutcome {
  const digestValue = requestDigest(input, context);
  const adapted = adaptation(channel, assistance);
  if (
    !context.tenantId ||
    !context.principalId ||
    !context.purpose ||
    !context.capability.id ||
    !context.capability.version
  ) {
    return {
      ok: false,
      channel,
      error: {
        code: "INVALID_CONTEXT",
        fields: ["trustedContext"],
        message: "trusted context is required",
      },
      requestDigest: digestValue,
      adaptation: adapted,
    };
  }
  const required = REQUIRED_FIELDS[input.intentType] ?? [];
  const missing = required.filter((field) => {
    const value = input.fields[field];
    return (
      value === undefined ||
      value === null ||
      (typeof value === "string" && value.trim() === "")
    );
  });
  if (!input.intentType || missing.length > 0) {
    return {
      ok: false,
      channel,
      error: {
        code: "INVALID_REQUIRED_INPUT",
        fields: missing.length ? missing : ["intentType"],
        message: "required input is missing",
      },
      requestDigest: digestValue,
      adaptation: adapted,
    };
  }
  const normalizedIntent: NormalizedIntent = {
    intentType: input.intentType,
    fields: JSON.parse(canonical(input.fields)) as Readonly<Record<string, unknown>>,
    capability: { ...context.capability },
    trustedContext: { ...context, capability: { ...context.capability } },
  };
  return {
    ok: true,
    channel,
    normalizedIntent,
    requestDigest: digestValue,
    result: {
      status: "created",
      intentType: input.intentType,
      capability: { ...context.capability },
    },
    adaptation: adapted,
    evidence: { confirmationRequired: true, simulationAvailable: true },
  };
}

export const createIntentForChannel = createIntent;
export const normalizeIntentCreation = createIntent;
