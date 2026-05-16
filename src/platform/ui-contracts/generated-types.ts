export const canonicalFieldTypeIds = [
  "text",
  "textarea",
  "number",
  "date",
  "time",
  "select",
  "combobox",
  "multi_select",
  "radio_group",
  "checkbox",
  "toggle",
  "slider",
  "repeater",
  "table",
  "matrix",
  "entity_picker",
  "tree_picker",
  "file_upload",
  "signature",
  "sensitive_reveal",
  "readonly",
] as const;

export type CanonicalFieldTypeId = (typeof canonicalFieldTypeIds)[number];

export const canonicalWidgetTypeIds = [
  "layout.section",
  "layout.stack",
  "layout.grid",
  "content.text",
  "content.markdown",
  "content.html",
  "content.callout",
  "content.linkList",
  "content.accordion",
  "data.metricTile",
  "data.progress",
  "data.labelValueList",
  "data.recordSummary",
  "data.checklist",
  "data.queueList",
  "data.table",
  "review.diff",
  "review.timeline",
  "workflow.actionBar",
  "workflow.reasonCapture",
  "viz.chart",
  "viz.graph",
  "ui.board",
  "media.viewer",
  "document.preview",
  "ui.tabs",
  "ui.stepper",
  "ui.modalDrawer",
  "ui.toastCenter",
  "form.subjectPicker",
  "data.employeeList",
] as const;

export type CanonicalWidgetTypeId = (typeof canonicalWidgetTypeIds)[number];

export type GeneratedTypeAlias<TCanonicalType extends string> = {
  sourceType: string;
  canonicalType: TCanonicalType;
  description: string;
  props?: Readonly<Record<string, unknown>>;
};

export type FieldTypeAliasDefinition = GeneratedTypeAlias<CanonicalFieldTypeId> & {
  source?: "legacy" | "domain" | "compound_recipe";
};

export type WidgetTypeAliasDefinition = GeneratedTypeAlias<CanonicalWidgetTypeId> & {
  source?: "legacy" | "catalog" | "domain_family";
  composition?: readonly CanonicalWidgetTypeId[];
};
