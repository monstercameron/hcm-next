/**
 * Canonical widget type IDs exposed to the AI as the only allowed vocabulary
 * for `widgets[].type`. This list is duplicated here (instead of imported
 * from `@hcm-next/ui-runtime`) so the ai-client workspace does not take a
 * runtime dependency on the registry. It MUST stay in sync with the
 * canonical entries declared in:
 *   src/platform/ui-runtime/registry.ts (canonicalWidgetDefinitions)
 *   src/platform/ui-contracts/generated-types.ts (canonicalWidgetTypeIds)
 *
 * A drift here would let the AI emit widget types that the registry cannot
 * render. The unit tests assert the null client only emits IDs from this
 * list, but cross-package drift detection lives at the system-test layer.
 */
export const CANONICAL_WIDGET_TYPE_IDS = [
  "content.callout",
  "content.markdown",
  "data.labelValueList",
  "data.checklist",
  "data.recordSummary",
  "data.metricTile",
  "data.table",
  "data.queueList",
  "data.progress",
  "review.diff",
  "review.timeline",
  "workflow.actionBar",
  "workflow.reasonCapture",
  "form.dynamicFieldGroup",
  "layout.section",
  "layout.stack",
  "layout.grid",
  "viz.chart",
  "viz.graph",
  "ui.tabs",
  "ui.stepper",
  "ui.modalDrawer",
  "media.viewer",
  "document.preview",
  "form.subjectPicker",
  "data.employeeList",
] as const;

export type CanonicalWidgetTypeId = (typeof CANONICAL_WIDGET_TYPE_IDS)[number];

const canonicalWidgetTypeIdSet: ReadonlySet<string> = new Set(
  CANONICAL_WIDGET_TYPE_IDS,
);

export function isCanonicalWidgetTypeId(value: string): value is CanonicalWidgetTypeId {
  return canonicalWidgetTypeIdSet.has(value);
}
