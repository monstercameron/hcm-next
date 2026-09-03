export type AccessibilitySupport = "supported" | "continuity" | "blocked";

export type AccessibilityMatrixCase = {
  id: string;
  assistiveTechnology: string;
  browser: string;
  locale: string;
  inputMode: "keyboard" | "voice" | "switch" | "touch";
  zoomPercent: 100 | 200 | 400;
  flow: string;
  support: AccessibilitySupport;
  testedAt: string;
  result: string;
  continuityRoute?: string;
};

export type AccessibilityMatrixSummary = {
  total: number;
  supported: number;
  continuity: number;
  blocked: number;
  criticalFailures: number;
};

export function summarizeAccessibilityMatrix(
  cases: readonly AccessibilityMatrixCase[],
): AccessibilityMatrixSummary {
  return cases.reduce<AccessibilityMatrixSummary>(
    (summary, item) => {
      summary.total += 1;
      summary[item.support] += 1;
      if (item.support === "blocked" || item.result === "critical_failure") {
        summary.criticalFailures += 1;
      }
      return summary;
    },
    { total: 0, supported: 0, continuity: 0, blocked: 0, criticalFailures: 0 },
  );
}

export function accessibleMatrixStatus(item: AccessibilityMatrixCase): string {
  if (item.support === "supported") return "Supported";
  if (item.support === "continuity" && item.continuityRoute) {
    return `Continuity route: ${item.continuityRoute}`;
  }
  return "Blocked — remediation required";
}

export function validateAccessibilityMatrixCase(
  item: AccessibilityMatrixCase,
): string[] {
  const errors: string[] = [];
  if (item.id.trim() === "") errors.push("missing_case_id");
  if (item.flow.trim() === "") errors.push("missing_critical_flow");
  if (item.testedAt.trim() === "") errors.push("missing_test_date");
  if (item.result.trim() === "") errors.push("missing_result");
  if (item.support === "continuity" && !item.continuityRoute?.trim()) {
    errors.push("continuity_route_required");
  }
  if (item.support === "supported" && item.result === "critical_failure") {
    errors.push("supported_case_cannot_have_critical_failure");
  }
  return errors;
}
