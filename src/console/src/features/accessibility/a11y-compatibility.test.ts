import { describe, expect, it } from "vitest";
import {
  accessibleMatrixStatus,
  summarizeAccessibilityMatrix,
  validateAccessibilityMatrixCase,
  type AccessibilityMatrixCase,
} from "./a11y-compatibility.js";

const baseCase: AccessibilityMatrixCase = {
  id: "promo-keyboard-en",
  assistiveTechnology: "NVDA",
  browser: "Chromium",
  locale: "en-US",
  inputMode: "keyboard",
  zoomPercent: 200,
  flow: "Promotion proposal",
  support: "supported",
  testedAt: "2026-09-03",
  result: "equivalent",
};

describe("A11Y-001 compatibility matrix", () => {
  it("summarizes supported, continuity and blocked combinations", () => {
    const summary = summarizeAccessibilityMatrix([
      baseCase,
      {
        ...baseCase,
        id: "voice-de",
        support: "continuity",
        continuityRoute: "assisted-review",
        result: "equivalent",
      },
      { ...baseCase, id: "switch-rtl", support: "blocked", result: "critical_failure" },
    ]);
    expect(summary).toEqual({
      total: 3,
      supported: 1,
      continuity: 1,
      blocked: 1,
      criticalFailures: 1,
    });
  });

  it("requires an explicit continuity route for unsupported combinations", () => {
    expect(
      validateAccessibilityMatrixCase({ ...baseCase, support: "continuity" }),
    ).toContain("continuity_route_required");
    expect(
      accessibleMatrixStatus({
        ...baseCase,
        support: "continuity",
        continuityRoute: "keyboard-assisted",
      }),
    ).toBe("Continuity route: keyboard-assisted");
  });

  it("rejects incomplete evidence and contradictory supported results", () => {
    expect(
      validateAccessibilityMatrixCase({
        ...baseCase,
        flow: "",
        result: "critical_failure",
      }),
    ).toEqual(["missing_critical_flow", "supported_case_cannot_have_critical_failure"]);
  });
});
