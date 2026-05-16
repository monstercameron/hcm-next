import { renderToStaticMarkup } from "react-dom/server";
import type { CSSProperties } from "react";
import type { ResolvedWidget } from "@hcm-next/ui-runtime";
import { describe, expect, it } from "vitest";
import {
  AccordionWidget,
  ChartWidget,
  MediaViewerWidget,
  QueueListWidget,
  RecordSummaryWidget,
  getWidgetFactoryEntry,
  renderWidgetFromRegistry,
  widgetRegistry,
} from "./index";

const resolvedWidget = (type: string): ResolvedWidget => {
  const instance = {
    id: `${type}-demo`,
    type,
    title: "Demo widget",
    props: {
      requests: [
        {
          id: "request-1",
          title: "Name change",
          employee: "Jane Rivera",
          risk: "low",
        },
      ],
      items: [
        {
          id: "item-1",
          label: "Question",
          answer: "Answer",
        },
      ],
      points: [
        {
          label: "Open",
          value: 4,
        },
      ],
      mediaType: "image",
      src: "/demo.png",
      style: {
        tone: "info",
        variant: "subtle",
      },
    },
  };

  return {
    instance,
    normalizedInstance: instance,
    definition: undefined,
    canonicalDefinition: undefined,
    originalType: type,
    canonicalType: type,
    bindings: {},
  } as ResolvedWidget;
};

describe("control-library widget registry", () => {
  const canonicalWidgetIds = [
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
  ];

  it("exposes every canonical widget type id", () => {
    expect(
      canonicalWidgetIds.map(
        (type) => getWidgetFactoryEntry(type, widgetRegistry)?.type,
      ),
    ).toEqual(canonicalWidgetIds);
  });

  it("returns registry entries by widget type", () => {
    const entry = getWidgetFactoryEntry("data.queueList", widgetRegistry);

    expect(entry?.componentName).toBe("QueueListWidget");
  });

  it("renders configured widgets from registry entries", () => {
    const element = renderWidgetFromRegistry(resolvedWidget("data.queueList"));

    expect(element?.type).toBe(QueueListWidget);
  });

  it("injects branding style props into registry-rendered widgets", () => {
    const style = {
      "--ui-control-accent": "#0044cc",
    } as CSSProperties;
    const element = renderWidgetFromRegistry(
      resolvedWidget("data.queueList"),
      widgetRegistry,
      {
        accentColor: "#0044cc",
        className: "tenant-brand-widget",
        style,
      },
    );

    expect(element?.props.styleProps.accentColor).toBe("#0044cc");
    expect(element?.props.styleProps.className).toContain("tenant-brand-widget");
    expect(
      element?.props.styleProps.style[
        "--ui-control-accent" as keyof typeof element.props.styleProps.style
      ],
    ).toBe("#0044cc");
  });

  it("normalizes exact legacy aliases to canonical widget entries", () => {
    expect(getWidgetFactoryEntry("queue.requestList")?.type).toBe("data.queueList");
    expect(getWidgetFactoryEntry("employee.summary")?.type).toBe("data.recordSummary");
    expect(getWidgetFactoryEntry("content.faq")?.type).toBe("content.accordion");
    expect(getWidgetFactoryEntry("data.graphChart")?.type).toBe("viz.chart");
    expect(getWidgetFactoryEntry("media.image")?.type).toBe("media.viewer");
    expect(getWidgetFactoryEntry("hcm.workerCard")).toBeUndefined();
  });

  it("renders normalized aliases with canonical components", () => {
    expect(renderWidgetFromRegistry(resolvedWidget("queue.requestList"))?.type).toBe(
      QueueListWidget,
    );
    expect(renderWidgetFromRegistry(resolvedWidget("employee.summary"))?.type).toBe(
      RecordSummaryWidget,
    );
    expect(renderWidgetFromRegistry(resolvedWidget("content.faq"))?.type).toBe(
      AccordionWidget,
    );
    expect(renderWidgetFromRegistry(resolvedWidget("data.metricGraph"))?.type).toBe(
      ChartWidget,
    );
    expect(renderWidgetFromRegistry(resolvedWidget("media.image"))?.type).toBe(
      MediaViewerWidget,
    );
  });

  it("renders parameterized direct components without workflow mutation handlers", () => {
    const markup = renderToStaticMarkup(
      <QueueListWidget
        config={{
          requests: [
            {
              id: "request-1",
              title: "Name change",
              employee: "Jane Rivera",
            },
          ],
        }}
      />,
    );

    expect(markup).toContain("Name change");
    expect(markup).toContain("Jane Rivera");
  });
});
