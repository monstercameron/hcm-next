import { describe, expect, it } from "vitest";
import {
  CHANNELS,
  createIntent,
  type IntentCreationInput,
  type TrustedContext,
} from "./index";

const input: IntentCreationInput = {
  intentType: "leave-request",
  fields: { reason: "family event", endDate: "2026-10-03", startDate: "2026-10-01" },
};
const context: TrustedContext = {
  tenantId: "tenant-1",
  principalId: "employee-7",
  purpose: "self-service",
  capability: { id: "intent.create", version: "2.1" },
};

describe("UX-008 channel parity", () => {
  it("TestTodo_UX_008_RegistryMatrixExact pins every channel contract", () => {
    const expected = {
      desktop: { inputMode: "pointer-keyboard", confirmation: "standard" },
      mobile: { inputMode: "touch", confirmation: "large-targets" },
      kiosk: { inputMode: "guided-touch", confirmation: "guided" },
      "accessibility-assisted": {
        inputMode: "screen-reader",
        confirmation: "announced",
      },
    } as const;
    const goldenDigest =
      "ac86d9b6970d0dcabd978e5d6115486525f8c1c37451ed1009e093d67fc9b233";

    for (const channel of CHANNELS) {
      const outcome = createIntent(channel, input, context);
      expect(outcome).toMatchObject({
        ok: true,
        channel,
        requestDigest: goldenDigest,
        adaptation: {
          ...expected[channel],
          simulation: "available",
        },
        evidence: { confirmationRequired: true, simulationAvailable: true },
      });
      if (outcome.ok) {
        expect(outcome.normalizedIntent).toEqual({
          intentType: input.intentType,
          fields: {
            endDate: "2026-10-03",
            reason: "family event",
            startDate: "2026-10-01",
          },
          capability: context.capability,
          trustedContext: context,
        });
        expect(outcome.result).toEqual({
          status: "created",
          intentType: input.intentType,
          capability: context.capability,
        });
      }
    }
  });

  it("TestIntentCreationChannelAccessibilityParity", () => {
    const outcomes = CHANNELS.map((channel) => createIntent(channel, input, context));
    expect(outcomes.every((outcome) => outcome.ok)).toBe(true);
    const successful = outcomes.filter((outcome) => outcome.ok);
    expect(new Set(successful.map((outcome) => outcome.requestDigest)).size).toBe(1);
    expect(
      new Set(successful.map((outcome) => JSON.stringify(outcome.normalizedIntent)))
        .size,
    ).toBe(1);
    expect(
      new Set(successful.map((outcome) => JSON.stringify(outcome.result))).size,
    ).toBe(1);
    expect(
      successful.every(
        (outcome) =>
          outcome.evidence.confirmationRequired && outcome.evidence.simulationAvailable,
      ),
    ).toBe(true);
    expect(
      successful.every(
        (outcome) =>
          outcome.normalizedIntent.trustedContext.principalId === context.principalId,
      ),
    ).toBe(true);
    expect(outcomes.map((outcome) => outcome.adaptation.inputMode)).toEqual([
      "pointer-keyboard",
      "touch",
      "guided-touch",
      "screen-reader",
    ]);
    const kiosk = outcomes[2]!;
    expect(kiosk.ok ? kiosk.adaptation.handoff?.authorityToken : undefined).toBe(false);
  });

  it.each(CHANNELS)(
    "TestTodo_UX_008_Golden preserves capability/version and digest (%s)",
    (channel) => {
      const outcome = createIntent(channel, input, context);
      expect(outcome.ok).toBe(true);
      if (outcome.ok) {
        expect(outcome.normalizedIntent.capability).toEqual(context.capability);
        expect(outcome.result.capability).toEqual(context.capability);
        expect(outcome.requestDigest).toMatch(/^[a-f0-9]{64}$/);
      }
    },
  );

  it("TestTodo_UX_008_Integration returns identical error semantics", () => {
    const invalid = { ...input, fields: { ...input.fields, startDate: "" } };
    const outcomes = CHANNELS.map((channel) => createIntent(channel, invalid, context));
    expect(outcomes.map((outcome) => outcome.ok)).toEqual([false, false, false, false]);
    expect(
      new Set(
        outcomes
          .filter((outcome) => !outcome.ok)
          .map((outcome) => JSON.stringify(outcome.error)),
      ).size,
    ).toBe(1);
    expect(new Set(outcomes.map((outcome) => outcome.requestDigest)).size).toBe(1);
  });

  it("TestTodo_UX_008_Security never delegates authority to a presentation channel", () => {
    const outcomes = CHANNELS.map((channel) => createIntent(channel, input, context));
    expect(outcomes.every((outcome) => outcome.ok)).toBe(true);
    expect(
      outcomes.some(
        (outcome) => outcome.ok && outcome.adaptation.handoff?.authorityToken,
      ),
    ).toBe(false);
  });

  it("TestTodo_UX_008_QRLinkAuthority rejects presentation links as authority", () => {
    const outcomes = CHANNELS.map((channel) => createIntent(channel, input, context));
    for (const outcome of outcomes) {
      expect(outcome.ok).toBe(true);
      // QR/short-link handoff is presentation-only: the sole handoff marker is
      // an explicit false authority token, and no channel can mint credentials.
      expect(outcome.adaptation.handoff?.authorityToken ?? false).toBe(false);
    }
    const kiosk = outcomes.find((outcome) => outcome.channel === "kiosk");
    expect(kiosk?.ok && kiosk.adaptation.handoff).toEqual({
      kind: "safe-resume",
      reference: "resume:opaque",
      authorityToken: false,
    });
  });

  it("TestTodo_UX_008_ErrorMatrixExact keeps QR/link variants on one error and digest", () => {
    const invalid = { ...input, fields: { ...input.fields, startDate: "" } };
    const expectedError = {
      code: "INVALID_REQUIRED_INPUT",
      fields: ["startDate"],
      message: "required input is missing",
    };
    const outcomes = CHANNELS.map((channel) => createIntent(channel, invalid, context));
    for (const outcome of outcomes) {
      expect(outcome).toMatchObject({ ok: false, error: expectedError });
    }
    expect(new Set(outcomes.map((outcome) => outcome.requestDigest)).size).toBe(1);
    expect(
      outcomes.every((outcome) => outcome.adaptation.simulation === "available"),
    ).toBe(true);
  });

  it("TestTodo_UX_008_Conformance requires confirmation and simulation on every channel", () => {
    for (const channel of CHANNELS) {
      const outcome = createIntent(channel, input, context);
      expect(outcome.ok && outcome.evidence).toEqual({
        confirmationRequired: true,
        simulationAvailable: true,
      });
    }
  });

  it("TestTodo_UX_008_Browser keeps adaptations outside the semantic payload", () => {
    const desktop = createIntent("desktop", input, context);
    const mobile = createIntent("mobile", input, context);
    expect(desktop.ok && mobile.ok).toBe(true);
    if (desktop.ok && mobile.ok)
      expect(mobile.normalizedIntent).toEqual(desktop.normalizedIntent);
  });

  it("assistance is attributed without changing semantic identity", () => {
    const normal = createIntent("desktop", input, context);
    const assisted = createIntent("accessibility-assisted", input, context, {
      kind: "representative",
      actorId: "rep-9",
    });
    expect(normal.ok && assisted.ok).toBe(true);
    if (normal.ok && assisted.ok) {
      expect(assisted.requestDigest).toBe(normal.requestDigest);
      expect(assisted.normalizedIntent).toEqual(normal.normalizedIntent);
      expect(assisted.adaptation.assistance?.actorId).toBe("rep-9");
    }
  });
});
