import type { ResolvedWidget } from "@hcm-next/ui-runtime";
import { cloneElement, type ReactElement } from "react";
import {
  AuditTimelineWidget,
  ApprovalDecisionPanelWidget,
  ChangeDiffWidget,
  EmployeeSummaryWidget,
  RequestQueueWidget,
  SimulationResultPanelWidget,
  type ApprovalActionConfig,
  type ApprovalDecisionPanelConfig,
  type AuditTimelineConfig,
  type ChangeDiffConfig,
  type EmployeeSummaryConfig,
  type RequestQueueConfig,
  type SimulationResultPanelConfig,
} from "./workflow-widgets";
import {
  CalloutContentWidget,
  FaqWidget,
  HtmlContentWidget,
  LabelValueListWidget,
  LinkListWidget,
  MarkdownContentWidget,
  TextContentWidget,
  faqItemsFromRecords,
  labelValueItemsFromRecords,
  linkItemsFromRecords,
  type CalloutContentConfig,
  type FaqConfig,
  type HtmlContentConfig,
  type LabelValueListConfig,
  type LinkListConfig,
  type MarkdownContentConfig,
  type TextContentConfig,
} from "./content-widgets";
import {
  ClusterBoardWidget,
  DataTableWidget,
  FilterableTableWidget,
  GraphChartWidget,
  MatrixWidget,
  MetricGraphWidget,
  MetricTileWidget,
  ProgressWidget,
  clusterBoardGroupsFromRecords,
  clusterBoardItemsFromRecords,
  matrixItemsFromRecords,
  matrixValuesFromConfig,
  metricPointsFromRecords,
  tableColumnsFromRecords,
  type ClusterBoardConfig,
  type DataTableConfig,
  type FilterableTableConfig,
  type GraphChartConfig,
  type MatrixConfig,
  type MetricGraphConfig,
  type MetricTileConfig,
  type ProgressConfig,
} from "./data-widgets";
import {
  NodeGraphWidget,
  OrgChartWidget,
  graphEdgesFromRecords,
  graphNodesFromRecords,
  orgChartNodesFromRecords,
  type NodeGraphConfig,
  type OrgChartConfig,
} from "./graph-widgets";
import {
  MediaWidget,
  PdfViewerWidget,
  type MediaConfig,
  type PdfViewerConfig,
} from "./media-widgets";
import { WidgetGrid, WidgetSection, WidgetStack } from "./primitives";
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

const requestQueueConfig = (widget: ResolvedWidget): RequestQueueConfig => {
  const props = widgetProps(widget);

  return {
    requests: recordsValue(props.requests).map((request) => ({
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
  const employee = objectValue(widget.bindings.employee?.value);
  const configuredFacts = recordsValue(props.facts).map(recordToLabelValueItem);
  const factsConfig = configuredFacts.length > 0 ? { facts: configuredFacts } : {};

  return {
    employee: {
      displayName: stringValue(employee.displayName, "Unknown employee"),
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

const approvalActions = (widget: ResolvedWidget): readonly ApprovalActionConfig[] =>
  (widget.instance.actions ?? []).map((action) => {
    const variant = action.variant === "quiet" ? "secondary" : action.variant;
    const variantConfig = variant === undefined ? {} : { variant };

    return {
      action: action.transition ?? action.command ?? action.action,
      label: action.label,
      disabled: false,
      ...variantConfig,
    };
  });

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

const simulationResultPanelConfig = (
  widget: ResolvedWidget,
): SimulationResultPanelConfig => {
  const props = widgetProps(widget);

  return {
    checks: recordsValue(props.checks).map((check) => ({
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
    events: recordsValue(props.events).map((event) => ({
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
  markdown: stringValue(widgetProps(widget).markdown),
});

const htmlContentConfig = (widget: ResolvedWidget): HtmlContentConfig => ({
  html: stringValue(widgetProps(widget).html),
});

const calloutContentConfig = (widget: ResolvedWidget): CalloutContentConfig => ({
  body: stringValue(widgetProps(widget).body),
});

const linkListConfig = (widget: ResolvedWidget): LinkListConfig => ({
  links: linkItemsFromRecords(recordsValue(widgetProps(widget).links)),
});

const faqConfig = (widget: ResolvedWidget): FaqConfig => ({
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

const metricGraphConfig = (widget: ResolvedWidget): MetricGraphConfig => {
  const props = widgetProps(widget);
  const variant = stringValue(props.variant, "bar");

  return {
    points: metricPointsFromRecords(props.points),
    variant: variant === "line" ? "line" : "bar",
  };
};

const graphChartConfig = (widget: ResolvedWidget): GraphChartConfig => ({
  series: recordsValue(widgetProps(widget).series).map((series) => ({
    id: stringValue(series.id, stringValue(series.label, "series")),
    label: stringValue(series.label, stringValue(series.id, "Series")),
    points: metricPointsFromRecords(series.points),
  })),
});

const dataTableConfig = (widget: ResolvedWidget): DataTableConfig => {
  const props = widgetProps(widget);

  return {
    columns: tableColumnsFromRecords(props.columns),
    rows: recordsValue(props.rows),
  };
};

const filterableTableConfig = (widget: ResolvedWidget): FilterableTableConfig => {
  const props = widgetProps(widget);

  return {
    columns: tableColumnsFromRecords(props.columns),
    rows: recordsValue(props.rows),
    riskFilterField: stringValue(props.riskFilterField, "risk"),
  };
};

const matrixConfig = (widget: ResolvedWidget): MatrixConfig => {
  const props = widgetProps(widget);

  return {
    rows: matrixItemsFromRecords(recordsValue(props.rows)),
    columns: matrixItemsFromRecords(recordsValue(props.columns)),
    values: matrixValuesFromConfig(props.values),
  };
};

const clusterBoardConfig = (widget: ResolvedWidget): ClusterBoardConfig => {
  const props = widgetProps(widget);

  return {
    groups: clusterBoardGroupsFromRecords(recordsValue(props.groups)),
    items: clusterBoardItemsFromRecords(recordsValue(props.items)),
  };
};

const orgChartConfig = (widget: ResolvedWidget): OrgChartConfig => ({
  nodes: orgChartNodesFromRecords(recordsValue(widgetProps(widget).nodes)),
});

const nodeGraphConfig = (widget: ResolvedWidget): NodeGraphConfig => {
  const props = widgetProps(widget);

  return {
    nodes: graphNodesFromRecords(props.nodes),
    edges: graphEdgesFromRecords(props.edges),
    label: stringValue(widget.instance.title, "Node graph"),
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

  if (widget.instance.type === "media.image") {
    return {
      mediaType: "image",
      src,
      alt: stringValue(props.alt, stringValue(widget.instance.title, "Image")),
    };
  }

  if (widget.instance.type === "media.audio") {
    return {
      mediaType: "audio",
      src,
      transcript,
    };
  }

  if (widget.instance.type === "media.video") {
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
  "layout.section": factory("layout.section", "WidgetSection", (widget) => (
    <WidgetSection
      title={widget.instance.title}
      description={widget.instance.description}
      {...widgetStyleProps(widget)}
    >
      <p>{valueToText(widgetProps(widget).body)}</p>
    </WidgetSection>
  )),
  "layout.stack": factory("layout.stack", "WidgetStack", (widget) => (
    <WidgetStack {...widgetStyleProps(widget)}>
      <p>{valueToText(widgetProps(widget).body)}</p>
    </WidgetStack>
  )),
  "layout.grid": factory("layout.grid", "WidgetGrid", (widget) => (
    <WidgetGrid {...widgetStyleProps(widget)}>
      <p>{valueToText(widgetProps(widget).body)}</p>
    </WidgetGrid>
  )),
  "queue.requestList": factory("queue.requestList", "RequestQueueWidget", (widget) => (
    <RequestQueueWidget
      config={requestQueueConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "employee.summary": factory("employee.summary", "EmployeeSummaryWidget", (widget) => (
    <EmployeeSummaryWidget
      config={employeeSummaryConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "change.diff": factory("change.diff", "ChangeDiffWidget", (widget) => (
    <ChangeDiffWidget
      config={changeDiffConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "approval.decisionPanel": factory(
    "approval.decisionPanel",
    "ApprovalDecisionPanelWidget",
    (widget) => (
      <ApprovalDecisionPanelWidget
        config={approvalDecisionPanelConfig(widget)}
        styleProps={widgetStyleProps(widget)}
      />
    ),
  ),
  "simulation.resultPanel": factory(
    "simulation.resultPanel",
    "SimulationResultPanelWidget",
    (widget) => (
      <SimulationResultPanelWidget
        config={simulationResultPanelConfig(widget)}
        styleProps={widgetStyleProps(widget)}
      />
    ),
  ),
  "audit.timeline": factory("audit.timeline", "AuditTimelineWidget", (widget) => (
    <AuditTimelineWidget
      config={auditTimelineConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
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
  "content.callout": factory("content.callout", "CalloutContentWidget", (widget) => (
    <CalloutContentWidget
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
  "content.metricTile": factory("content.metricTile", "MetricTileWidget", (widget) => (
    <MetricTileWidget
      config={metricTileConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "content.labelValueList": factory(
    "content.labelValueList",
    "LabelValueListWidget",
    (widget) => (
      <LabelValueListWidget
        config={labelValueListConfig(widget)}
        styleProps={widgetStyleProps(widget)}
      />
    ),
  ),
  "content.faq": factory("content.faq", "FaqWidget", (widget) => (
    <FaqWidget config={faqConfig(widget)} styleProps={widgetStyleProps(widget)} />
  )),
  "data.progress": factory("data.progress", "ProgressWidget", (widget) => (
    <ProgressWidget
      config={progressConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.metricGraph": factory("data.metricGraph", "MetricGraphWidget", (widget) => (
    <MetricGraphWidget
      config={metricGraphConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.graphChart": factory("data.graphChart", "GraphChartWidget", (widget) => (
    <GraphChartWidget
      config={graphChartConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.nodeGraph": factory("data.nodeGraph", "NodeGraphWidget", (widget) => (
    <NodeGraphWidget
      config={nodeGraphConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.orgChart": factory("data.orgChart", "OrgChartWidget", (widget) => (
    <OrgChartWidget
      config={orgChartConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.table": factory("data.table", "DataTableWidget", (widget) => (
    <DataTableWidget
      config={dataTableConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "data.filterableTable": factory(
    "data.filterableTable",
    "FilterableTableWidget",
    (widget) => (
      <FilterableTableWidget
        config={filterableTableConfig(widget)}
        styleProps={widgetStyleProps(widget)}
      />
    ),
  ),
  "data.matrix": factory("data.matrix", "MatrixWidget", (widget) => (
    <MatrixWidget config={matrixConfig(widget)} styleProps={widgetStyleProps(widget)} />
  )),
  "data.clusterBoard": factory("data.clusterBoard", "ClusterBoardWidget", (widget) => (
    <ClusterBoardWidget
      config={clusterBoardConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
  "media.image": factory("media.image", "MediaWidget", (widget) => (
    <MediaWidget config={mediaConfig(widget)} styleProps={widgetStyleProps(widget)} />
  )),
  "media.audio": factory("media.audio", "MediaWidget", (widget) => (
    <MediaWidget config={mediaConfig(widget)} styleProps={widgetStyleProps(widget)} />
  )),
  "media.video": factory("media.video", "MediaWidget", (widget) => (
    <MediaWidget config={mediaConfig(widget)} styleProps={widgetStyleProps(widget)} />
  )),
  "media.pdf": factory("media.pdf", "PdfViewerWidget", (widget) => (
    <PdfViewerWidget
      config={pdfViewerConfig(widget)}
      styleProps={widgetStyleProps(widget)}
    />
  )),
} satisfies WidgetFactoryRegistry;

export const getWidgetFactoryEntry = (
  type: string,
  registry: WidgetFactoryRegistry = widgetRegistry,
): WidgetFactoryEntry | undefined => registry[type];

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
