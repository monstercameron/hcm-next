import type { ResolvedWidget } from "@hcm-next/ui-runtime";
import { cloneElement, type ReactElement } from "react";
import {
  ActionBarWidget,
  ChecklistWidget,
  DiffViewerWidget,
  QueueListWidget,
  ReasonCaptureWidget,
  RecordSummaryWidget,
  TimelineWidget,
  type ApprovalActionConfig,
  type ApprovalDecisionPanelConfig,
  type AuditTimelineConfig,
  type ChangeDiffConfig,
  type EmployeeSummaryConfig,
  type ReasonCaptureConfig,
  type RequestQueueConfig,
  type SimulationResultPanelConfig,
} from "./workflow-widgets";
import {
  AccordionWidget,
  CalloutWidget,
  HtmlContentWidget,
  LabelValueListWidget,
  LinkListWidget,
  MarkdownContentWidget,
  TextContentWidget,
  faqItemsFromRecords,
  labelValueItemsFromRecords,
  linkItemsFromRecords,
  type CalloutContentConfig,
  type HtmlContentConfig,
  type LabelValueListConfig,
  type LinkListConfig,
  type MarkdownContentConfig,
  type TextContentConfig,
} from "./content-widgets";
import {
  BoardWidget,
  ChartWidget,
  DataTableWidget,
  MetricTileWidget,
  ProgressWidget,
  clusterBoardGroupsFromRecords,
  clusterBoardItemsFromRecords,
  metricPointsFromRecords,
  tableColumnsFromRecords,
  type ChartConfig,
  type ClusterBoardConfig,
  type DataTableConfig,
  type MetricTileConfig,
  type ProgressConfig,
} from "./data-widgets";
import {
  GraphWidget,
  graphEdgesFromRecords,
  graphNodesFromRecords,
  orgChartNodesFromRecords,
  type GraphConfig,
  type NodeGraphConfig,
} from "./graph-widgets";
import {
  DocumentPreviewWidget,
  MediaViewerWidget,
  type DocumentPreviewConfig,
  type MediaConfig,
  type PdfViewerConfig,
} from "./media-widgets";
import { GridLayout, SectionLayout, StackLayout } from "./primitives";
import {
  ModalDrawerWidget,
  StepperWidget,
  TabsWidget,
  ToastCenterWidget,
  stepsConfigFromRecords,
  tabsConfigFromRecords,
  toastsConfigFromRecords,
  type ModalDrawerConfig,
  type StepperConfig,
  type TabsConfig,
  type ToastCenterConfig,
} from "./ui-widgets";
import type {
  LabelValueItem,
  WidgetFactoryEntry,
  WidgetFactoryRegistry,
  WidgetRecord,
  WidgetStyleProps,
} from "./types";
import {
  numberValue,
  objectValue,
  recordsValue,
  mergeWidgetStyleProps,
  stringArrayValue,
  stringValue,
  stylePropsFromConfig,
  valueToText,
} from "./utils";

export * from "./types";
export * from "./utils";
export * from "./primitives";
export * from "./workflow-widgets";
export * from "./content-widgets";
export * from "./data-widgets";
export * from "./graph-widgets";
export * from "./media-widgets";
export * from "./ui-widgets";

const widgetProps = (widget: ResolvedWidget): WidgetRecord =>
  objectValue(widget.instance.props);

const widgetStyleProps = (widget: ResolvedWidget) =>
  stylePropsFromConfig(widgetProps(widget).style);

type WidgetElementStyleProps = WidgetStyleProps & {
  styleProps?: WidgetStyleProps | undefined;
};

const widgetStyleKeys = [
  "className",
  "style",
  "tone",
  "variant",
  "density",
  "accentColor",
  "surfaceColor",
  "mutedSurfaceColor",
  "activeSurfaceColor",
  "borderColor",
  "borderStrongColor",
  "textColor",
  "mutedTextColor",
  "radius",
  "controlHeight",
  "shadow",
  "hoverLift",
  "hoverScale",
  "motionFast",
  "motionMedium",
] as const;

const widgetStylePropsFromElementProps = (
  props: WidgetElementStyleProps,
): WidgetStyleProps => {
  const styleProps: WidgetStyleProps = {};

  for (const styleKey of widgetStyleKeys) {
    const value = props[styleKey];

    if (value !== undefined) {
      Object.assign(styleProps, { [styleKey]: value });
    }
  }

  return styleProps;
};

const withWidgetBrandingStyleProps = (
  element: JSX.Element | undefined,
  brandingStyleProps: WidgetStyleProps | undefined,
): JSX.Element | undefined => {
  if (element === undefined || brandingStyleProps === undefined) {
    return element;
  }

  const typedElement = element as ReactElement<WidgetElementStyleProps>;
  const existingStyleProps =
    typedElement.props.styleProps ??
    widgetStylePropsFromElementProps(typedElement.props);
  const mergedStyleProps = mergeWidgetStyleProps(
    brandingStyleProps,
    existingStyleProps,
  );

  return cloneElement(typedElement, {
    ...mergedStyleProps,
    styleProps: mergedStyleProps,
  });
};

const recordToLabelValueItem = (record: WidgetRecord): LabelValueItem => ({
  label: stringValue(record.label, "Value"),
  value: record.value,
  detail: stringValue(record.detail),
});

const recordsFromFirstAvailable = (
  ...values: readonly unknown[]
): readonly WidgetRecord[] => {
  for (const value of values) {
    const records = recordsValue(value);

    if (records.length > 0) {
      return records;
    }
  }

  return [];
};

const requestQueueConfig = (widget: ResolvedWidget): RequestQueueConfig => {
  const props = widgetProps(widget);

  return {
    requests: recordsFromFirstAvailable(props.requests, props.items).map((request) => ({
      id: stringValue(request.id, stringValue(request.title, "request")),
      title: stringValue(request.title, "Untitled request"),
      employee: stringValue(request.employee, "No subject"),
      state: stringValue(request.state),
      risk: stringValue(request.risk, "info"),
      due: stringValue(request.due),
      detail: stringValue(request.detail),
    })),
    emptyLabel: stringValue(props.emptyLabel),
  };
};

const employeeSummaryConfig = (widget: ResolvedWidget): EmployeeSummaryConfig => {
  const props = widgetProps(widget);
  const employee = {
    ...objectValue(widget.bindings.employee?.value),
    ...objectValue(props.record),
  };
  const configuredFacts = recordsFromFirstAvailable(props.facts, props.items).map(
    recordToLabelValueItem,
  );
  const factsConfig = configuredFacts.length > 0 ? { facts: configuredFacts } : {};

  return {
    employee: {
      displayName: stringValue(
        props.displayName,
        stringValue(employee.displayName, stringValue(widget.instance.title, "Record")),
      ),
      jobTitle: stringValue(employee.jobTitle, "Not set"),
      department: stringValue(employee.department, "Not set"),
      manager: stringValue(employee.manager, "Not set"),
      avatarLabel: stringValue(props.avatarLabel),
    },
    ...factsConfig,
  };
};

const diffRowsFromBindings = (widget: ResolvedWidget): ChangeDiffConfig["rows"] => {
  const current = objectValue(widget.bindings.current?.value);
  const proposed = objectValue(widget.bindings.proposed?.value);
  const keys = Array.from(new Set([...Object.keys(current), ...Object.keys(proposed)]));

  return keys.map((key) => ({
    id: key,
    label: key,
    current: current[key],
    proposed: proposed[key],
  }));
};

const changeDiffConfig = (widget: ResolvedWidget): ChangeDiffConfig => {
  const props = widgetProps(widget);
  const configuredRows = recordsValue(props.rows).map((row) => ({
    id: stringValue(row.id, stringValue(row.label, "field")),
    label: stringValue(row.label, stringValue(row.id, "Field")),
    current: row.current,
    proposed: row.proposed,
    status: stringValue(row.status),
  }));

  return {
    rows: configuredRows.length > 0 ? configuredRows : diffRowsFromBindings(widget),
    emptyLabel: stringValue(props.emptyLabel),
  };
};

const configuredApprovalActions = (value: unknown): readonly ApprovalActionConfig[] =>
  recordsValue(value).map((action) => ({
    action: stringValue(action.action, stringValue(action.transition)),
    label: stringValue(action.label, stringValue(action.action, "Action")),
    disabled: action.disabled === true,
    variant:
      action.variant === "primary" ||
      action.variant === "secondary" ||
      action.variant === "danger"
        ? action.variant
        : "secondary",
    reason: stringValue(action.reason),
  }));

const approvalActions = (widget: ResolvedWidget): readonly ApprovalActionConfig[] => {
  const props = widgetProps(widget);
  const configuredActions = configuredApprovalActions(props.actions);

  if (configuredActions.length > 0) {
    return configuredActions;
  }

  return (widget.instance.actions ?? []).map((action) => {
    const variant = action.variant === "quiet" ? "secondary" : action.variant;
    const variantConfig = variant === undefined ? {} : { variant };

    return {
      action: action.transition ?? action.command ?? action.action,
      label: action.label,
      disabled: false,
      ...variantConfig,
    };
  });
};

const approvalDecisionPanelConfig = (
  widget: ResolvedWidget,
): ApprovalDecisionPanelConfig => {
  const props = widgetProps(widget);

  return {
    actions: approvalActions(widget),
    selectedAction: stringValue(props.selectedAction),
    description: stringValue(
      props.description,
      "Decision actions are bound to workflow transitions. Server state and RBAC remain authoritative.",
    ),
    selectedLabel: stringValue(props.selectedLabel, "Selected transition"),
  };
};

const reasonCaptureConfig = (widget: ResolvedWidget): ReasonCaptureConfig => {
  const props = widgetProps(widget);

  return {
    label: stringValue(props.label, "Reason"),
    placeholder: stringValue(props.placeholder),
    value: stringValue(props.value),
    helperText: stringValue(props.helperText, stringValue(props.description)),
    required: props.required === true,
    reasons:
      stringArrayValue(props.reasons).length > 0
        ? stringArrayValue(props.reasons)
        : recordsValue(props.reasons).map((reason) =>
            stringValue(reason.label, stringValue(reason.value)),
          ),
  };
};

const simulationResultPanelConfig = (
  widget: ResolvedWidget,
): SimulationResultPanelConfig => {
  const props = widgetProps(widget);

  return {
    checks: recordsFromFirstAvailable(props.checks, props.items).map((check) => ({
      id: stringValue(check.id, stringValue(check.label, "check")),
      label: stringValue(check.label, "Check"),
      detail: stringValue(check.detail),
      status: stringValue(check.status, "info"),
    })),
    emptyLabel: stringValue(props.emptyLabel),
  };
};

const auditTimelineConfig = (widget: ResolvedWidget): AuditTimelineConfig => {
  const props = widgetProps(widget);

  return {
    events: recordsFromFirstAvailable(props.events, props.items).map((event) => ({
      id: stringValue(event.id, `${stringValue(event.at)}-${stringValue(event.label)}`),
      at: stringValue(event.at),
      label: stringValue(event.label, "Event"),
      actor: stringValue(event.actor),
      detail: stringValue(event.detail),
      status: stringValue(event.status),
    })),
    emptyLabel: stringValue(props.emptyLabel),
  };
};

const textContentConfig = (widget: ResolvedWidget): TextContentConfig => {
  const props = widgetProps(widget);

  return {
    body: stringValue(props.body),
    visibility: stringValue(props.visibility),
  };
};

const markdownContentConfig = (widget: ResolvedWidget): MarkdownContentConfig => ({
  markdown: stringValue(
    widgetProps(widget).markdown,
    stringValue(widgetProps(widget).body),
  ),
});

const htmlContentConfig = (widget: ResolvedWidget): HtmlContentConfig => ({
  html: stringValue(widgetProps(widget).html, stringValue(widgetProps(widget).body)),
});

const calloutContentConfig = (widget: ResolvedWidget): CalloutContentConfig => ({
  body: stringValue(widgetProps(widget).body),
});

const linkListConfig = (widget: ResolvedWidget): LinkListConfig => ({
  links: linkItemsFromRecords(recordsValue(widgetProps(widget).links)),
});

const accordionConfig = (widget: ResolvedWidget) => ({
  items: faqItemsFromRecords(widgetProps(widget).items),
});

const labelValueListConfig = (widget: ResolvedWidget): LabelValueListConfig => ({
  items: labelValueItemsFromRecords(recordsValue(widgetProps(widget).items)),
});

const metricTileConfig = (widget: ResolvedWidget): MetricTileConfig => {
  const props = widgetProps(widget);

  return {
    label: stringValue(props.label, stringValue(widget.instance.title, "Metric")),
    value: props.value,
    detail: stringValue(props.detail),
  };
};

const progressConfig = (widget: ResolvedWidget): ProgressConfig => {
  const props = widgetProps(widget);

  return {
    value: numberValue(props.value, 0),
    label: stringValue(props.label),
    max: numberValue(props.max, 100),
  };
};

const chartConfig = (widget: ResolvedWidget): ChartConfig => {
  const props = widgetProps(widget);
  const series = recordsValue(props.series).map((item) => ({
    id: stringValue(item.id, stringValue(item.label, "series")),
    label: stringValue(item.label, stringValue(item.id, "Series")),
    points: metricPointsFromRecords(item.points),
  }));
  const variant = stringValue(
    props.variant,
    widget.instance.type.startsWith("viz.")
      ? widget.instance.type.slice("viz.".length)
      : "bar",
  );

  return {
    points: metricPointsFromRecords(props.points),
    series,
    variant: variant === "line" || variant === "area" ? variant : "bar",
    emptyLabel: stringValue(props.emptyLabel),
  };
};

const dataTableConfig = (widget: ResolvedWidget): DataTableConfig => {
  const props = widgetProps(widget);
  const filterable =
    props.filterable === true || widget.instance.type === "data.filterableTable";

  return {
    columns: tableColumnsFromRecords(props.columns),
    rows: recordsValue(props.rows),
    filterable,
    riskFilterField: stringValue(props.riskFilterField, "risk"),
  };
};

const clusterBoardConfig = (widget: ResolvedWidget): ClusterBoardConfig => {
  const props = widgetProps(widget);

  return {
    groups: clusterBoardGroupsFromRecords(recordsValue(props.groups)),
    items: clusterBoardItemsFromRecords(recordsValue(props.items)),
  };
};

const nodeGraphConfig = (widget: ResolvedWidget): NodeGraphConfig => {
  const props = widgetProps(widget);

  return {
    nodes: graphNodesFromRecords(props.nodes),
    edges: graphEdgesFromRecords(props.edges),
    label: stringValue(widget.instance.title, "Node graph"),
  };
};

const graphConfig = (widget: ResolvedWidget): GraphConfig => {
  const props = widgetProps(widget);
  const configuredGraphType = stringValue(props.graphType);
  const graphType =
    configuredGraphType.length > 0
      ? configuredGraphType
      : widget.instance.type === "data.orgChart" ||
          widget.instance.type.toLowerCase().includes("orgchart")
        ? "org"
        : "node";

  return {
    ...nodeGraphConfig(widget),
    nodes:
      graphType === "org"
        ? orgChartNodesFromRecords(recordsValue(props.nodes))
        : graphNodesFromRecords(props.nodes),
    graphType,
  };
};

const pdfViewerConfig = (widget: ResolvedWidget): PdfViewerConfig => {
  const props = widgetProps(widget);

  return {
    src: stringValue(props.src),
    title: stringValue(widget.instance.title, "PDF preview"),
    label: stringValue(props.label, "PDF preview"),
    pageCount: numberValue(props.pageCount, 4),
  };
};

const mediaConfig = (widget: ResolvedWidget): MediaConfig => {
  const props = widgetProps(widget);
  const src = stringValue(props.src);
  const transcript = stringValue(props.transcript);
  const configuredMediaType = stringValue(props.mediaType);
  const mediaType =
    configuredMediaType.length > 0
      ? configuredMediaType
      : widget.instance.type.startsWith("media.")
        ? widget.instance.type.slice("media.".length)
        : "pdf";

  if (mediaType === "image") {
    return {
      mediaType: "image",
      src,
      alt: stringValue(props.alt, stringValue(widget.instance.title, "Image")),
    };
  }

  if (mediaType === "audio") {
    return {
      mediaType: "audio",
      src,
      transcript,
    };
  }

  if (mediaType === "video") {
    return {
      mediaType: "video",
      src,
      transcript,
    };
  }

  return {
    mediaType: "pdf",
    ...pdfViewerConfig(widget),
  };
};

const documentPreviewConfig = (widget: ResolvedWidget): DocumentPreviewConfig => {
  const props = widgetProps(widget);

  return {
    ...pdfViewerConfig(widget),
    documentType: stringValue(props.documentType, stringValue(props.type)),
    status: stringValue(props.status),
  };
};

const tabsConfig = (widget: ResolvedWidget): TabsConfig => {
  const props = widgetProps(widget);

  return {
    tabs: tabsConfigFromRecords(props.tabs ?? props.items),
    selectedId: stringValue(props.selectedId),
    emptyLabel: stringValue(props.emptyLabel),
  };
};

const stepperConfig = (widget: ResolvedWidget): StepperConfig => {
  const props = widgetProps(widget);

  return {
    steps: stepsConfigFromRecords(props.steps ?? props.items),
    currentStepId: stringValue(props.currentStepId, stringValue(props.selectedId)),
    emptyLabel: stringValue(props.emptyLabel),
  };
};

const modalDrawerConfig = (widget: ResolvedWidget): ModalDrawerConfig => {
  const props = widgetProps(widget);
  const placement = stringValue(props.placement);

  return {
    title: stringValue(props.title, stringValue(widget.instance.title, "Details")),
    body: stringValue(props.body, stringValue(widget.instance.description)),
    openLabel: stringValue(props.openLabel),
    closeLabel: stringValue(props.closeLabel),
    defaultOpen: props.defaultOpen === true || props.open === true,
    placement: placement === "drawer" ? "drawer" : "modal",
  };
};

const toastCenterConfig = (widget: ResolvedWidget): ToastCenterConfig => {
  const props = widgetProps(widget);

  return {
    toasts: toastsConfigFromRecords(props.toasts ?? props.items),
    emptyLabel: stringValue(props.emptyLabel),
  };
};

const factory = (
  type: string,
  componentName: string,
  render: WidgetFactoryEntry["render"],
): WidgetFactoryEntry => ({
  type,
  componentName,
  render,
});

export const widgetRegistry = {
  "layout.section": factory("layout.section", "SectionLayout", (widget) => (
    <SectionLayout
      title={widget.instance.title}
      description={widget.instance.description}
      {...widgetStyleProps(widget)}
    >
      <p>{valueToText(widgetProps(widget).body)}</p>
    </SectionLayout>
  )),
  "layout.stack": factory("layout.stack", "StackLayout", (widget) => (
    <StackLayout {...widgetStyleProps(widget)}>
      <p>{valueToText(widgetProps(widget).body)}</p>
    </StackLayout>
  )),
  "layout.grid": factory("layout.grid", "GridLayout", (widget) => (
    <GridLayout {...widgetStyleProps(widget)}>
      <p>{valueToText(widgetProps(widget).body)}</p>
    </GridLayout>
  )),
  "content.text": factory("content.text", "TextContentWidget", (widget) => (
    <TextContentWidget
      config={textContentConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "content.markdown": factory("content.markdown", "MarkdownContentWidget", (widget) => (
    <MarkdownContentWidget
      config={markdownContentConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "content.html": factory("content.html", "HtmlContentWidget", (widget) => (
    <HtmlContentWidget
      config={htmlContentConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "content.callout": factory("content.callout", "CalloutWidget", (widget) => (
    <CalloutWidget
      config={calloutContentConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "content.linkList": factory("content.linkList", "LinkListWidget", (widget) => (
    <LinkListWidget
      config={linkListConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "content.accordion": factory("content.accordion", "AccordionWidget", (widget) => (
    <AccordionWidget
      config={accordionConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.metricTile": factory("data.metricTile", "MetricTileWidget", (widget) => (
    <MetricTileWidget
      config={metricTileConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.progress": factory("data.progress", "ProgressWidget", (widget) => (
    <ProgressWidget
      config={progressConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.labelValueList": factory(
    "data.labelValueList",
    "LabelValueListWidget",
    (widget) => (
      <LabelValueListWidget
        config={labelValueListConfig(widget)}
        styleProps={widgetStyleProps(widget)}
      />
    ),
  ),
  "data.recordSummary": factory(
    "data.recordSummary",
    "RecordSummaryWidget",
    (widget) => (
      <RecordSummaryWidget
        config={employeeSummaryConfig(widget)}
        styleProps={widgetStyleProps(widget)}
      />
    ),
  ),
  "data.checklist": factory("data.checklist", "ChecklistWidget", (widget) => (
    <ChecklistWidget
      config={simulationResultPanelConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.queueList": factory("data.queueList", "QueueListWidget", (widget) => (
    <QueueListWidget
      config={requestQueueConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.table": factory("data.table", "DataTableWidget", (widget) => (
    <DataTableWidget
      config={dataTableConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "review.diff": factory("review.diff", "DiffViewerWidget", (widget) => (
    <DiffViewerWidget
      config={changeDiffConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "review.timeline": factory("review.timeline", "TimelineWidget", (widget) => (
    <TimelineWidget
      config={auditTimelineConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "workflow.actionBar": factory("workflow.actionBar", "ActionBarWidget", (widget) => (
    <ActionBarWidget
      config={approvalDecisionPanelConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "workflow.reasonCapture": factory(
    "workflow.reasonCapture",
    "ReasonCaptureWidget",
    (widget) => (
      <ReasonCaptureWidget
        config={reasonCaptureConfig(widget)}
        styleProps={widgetStyleProps(widget)}
      />
    ),
  ),
  "viz.chart": factory("viz.chart", "ChartWidget", (widget) => (
    <ChartWidget config={chartConfig(widget)} styleProps={widgetStyleProps(widget)} />
  )),
  "viz.graph": factory("viz.graph", "GraphWidget", (widget) => (
    <GraphWidget config={graphConfig(widget)} styleProps={widgetStyleProps(widget)} />
  )),
  "ui.board": factory("ui.board", "BoardWidget", (widget) => (
    <BoardWidget
      config={clusterBoardConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "media.viewer": factory("media.viewer", "MediaViewerWidget", (widget) => (
    <MediaViewerWidget
      config={mediaConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "document.preview": factory("document.preview", "DocumentPreviewWidget", (widget) => (
    <DocumentPreviewWidget
      config={documentPreviewConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "ui.tabs": factory("ui.tabs", "TabsWidget", (widget) => (
    <TabsWidget config={tabsConfig(widget)} styleProps={widgetStyleProps(widget)} />
  )),
  "ui.stepper": factory("ui.stepper", "StepperWidget", (widget) => (
    <StepperWidget
      config={stepperConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "ui.modalDrawer": factory("ui.modalDrawer", "ModalDrawerWidget", (widget) => (
    <ModalDrawerWidget
      config={modalDrawerConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "ui.toastCenter": factory("ui.toastCenter", "ToastCenterWidget", (widget) => (
    <ToastCenterWidget
      config={toastCenterConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
} satisfies WidgetFactoryRegistry;

const exactWidgetAliases: Readonly<Record<string, string>> = {
  "queue.requestList": "data.queueList",
  "employee.summary": "data.recordSummary",
  "change.diff": "review.diff",
  "approval.decisionPanel": "workflow.actionBar",
  "simulation.resultPanel": "data.checklist",
  "audit.timeline": "review.timeline",
  "content.faq": "content.accordion",
  "content.metricTile": "data.metricTile",
  "content.labelValueList": "data.labelValueList",
  "data.filterableTable": "data.table",
  "data.metricGraph": "viz.chart",
  "data.graphChart": "viz.chart",
  "data.nodeGraph": "viz.graph",
  "data.orgChart": "viz.graph",
  "data.clusterBoard": "ui.board",
  "data.matrix": "data.table",
  "ui.kanbanBoard": "ui.board",
  "media.image": "media.viewer",
  "media.audio": "media.viewer",
  "media.video": "media.viewer",
  "media.pdf": "media.viewer",
};

export const normalizeWidgetType = (type: string): string => {
  const exactAlias = exactWidgetAliases[type];

  if (exactAlias !== undefined) {
    return exactAlias;
  }

  return type;
};

export const getWidgetFactoryEntry = (
  type: string,
  registry: WidgetFactoryRegistry = widgetRegistry,
): WidgetFactoryEntry | undefined =>
  registry[type] ?? registry[normalizeWidgetType(type)];

/** Builds a configured React element for a resolved runtime widget. */
export const renderWidgetFromRegistry = (
  widget: ResolvedWidget,
  registry: WidgetFactoryRegistry = widgetRegistry,
  brandingStyleProps?: WidgetStyleProps,
): JSX.Element | undefined =>
  withWidgetBrandingStyleProps(
    getWidgetFactoryEntry(widget.instance.type, registry)?.render(widget),
    brandingStyleProps,
  );

export const createWidgetElement = renderWidgetFromRegistry;
