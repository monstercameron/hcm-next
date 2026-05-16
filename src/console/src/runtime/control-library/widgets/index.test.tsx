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
  SectionLayout,
  getWidgetFactoryEntry,
  renderWidgetFromRegistry,
  widgetRegistry,
} from "./index";
import { PageFormProvider } from "../../page-form-context.js";

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
      label: "Select employee",
      required: false,
      options: [
        { id: "emp_1", displayName: "Jane Rivera", jobTitle: "Engineer" },
        { id: "emp_2", displayName: "Sam Patel", department: "Platform" },
      ],
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
    "form.subjectPicker",
    "data.employeeList",
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
    const markup = renderToStaticMarkup(element);

    expect(element?.props.brandingStyleProps.accentColor).toBe("#0044cc");
    expect(element?.props.brandingStyleProps.className).toContain(
      "tenant-brand-widget",
    );
    expect(markup).toContain("tenant-brand-widget");
    expect(markup).toContain("--ui-control-accent:#0044cc");
  });

  it("applies branding style props to every canonical registry-rendered widget", () => {
    for (const type of canonicalWidgetIds) {
      const element = renderWidgetFromRegistry(resolvedWidget(type), widgetRegistry, {
        accentColor: "#0044cc",
        className: "tenant-brand-widget",
        style: {
          backgroundColor: "rgb(240, 245, 255)",
        },
      });

      expect(element, type).toBeDefined();

      const markup = renderToStaticMarkup(element!);

      expect(markup, type).toContain("tenant-brand-widget");
      expect(markup, type).toContain("--ui-control-accent:#0044cc");
      expect(markup, type).toMatch(/background-color:rgb\(240,\s?245,\s?255\)/);
    }
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

  it("accepts branding style props on direct widget and layout components", () => {
    const brandingStyleProps = {
      accentColor: "#0044cc",
      className: "tenant-brand-widget",
      style: {
        backgroundColor: "rgb(240, 245, 255)",
      },
    };
    const queueMarkup = renderToStaticMarkup(
      <QueueListWidget
        brandingStyleProps={brandingStyleProps}
        config={{
          requests: [
            {
              id: "request-1",
              title: "Name change",
              employee: "Jane Rivera",
            },
          ],
        }}
        styleProps={{ className: "local-widget" }}
      />,
    );
    const layoutMarkup = renderToStaticMarkup(
      <SectionLayout
        brandingStyleProps={brandingStyleProps}
        className="local-layout"
        title="Direct layout"
      >
        <p>Layout body</p>
      </SectionLayout>,
    );

    expect(queueMarkup).toContain("tenant-brand-widget");
    expect(queueMarkup).toContain("local-widget");
    expect(queueMarkup).toContain("--ui-control-accent:#0044cc");
    expect(layoutMarkup).toContain("tenant-brand-widget");
    expect(layoutMarkup).toContain("local-layout");
    expect(layoutMarkup).toContain("--ui-control-accent:#0044cc");
  });

  it("renders the recordSummary with PageFormContext when recordId is $selectedSubject", () => {
    const instance = {
      id: "summary-1",
      type: "data.recordSummary",
      title: "Selected employee",
      props: {
        recordId: "$selectedSubject",
      },
    };
    const widget = {
      instance,
      normalizedInstance: instance,
      definition: undefined,
      canonicalDefinition: undefined,
      originalType: instance.type,
      canonicalType: instance.type,
      bindings: {},
    } as ResolvedWidget;

    const element = renderWidgetFromRegistry(widget);
    const markup = renderToStaticMarkup(
      <PageFormProvider
        initialAvailableSubjects={[
          {
            id: "emp_42",
            displayName: "Leo Park",
            jobTitle: "IT Manager",
            department: "IT",
            manager: "Pat Khan",
          },
        ]}
        initialSelectedSubjectId="emp_42"
      >
        {element}
      </PageFormProvider>,
    );

    expect(markup).toContain("Leo Park");
    expect(markup).toContain("IT Manager");
    expect(markup).toContain("Pat Khan");
  });

  it("renders the Not set placeholder when nothing is picked", () => {
    const instance = {
      id: "summary-2",
      type: "data.recordSummary",
      title: "Selected employee",
      props: { recordId: "$selectedSubject" },
    };
    const widget = {
      instance,
      normalizedInstance: instance,
      definition: undefined,
      canonicalDefinition: undefined,
      originalType: instance.type,
      canonicalType: instance.type,
      bindings: {},
    } as ResolvedWidget;

    const element = renderWidgetFromRegistry(widget);
    const markup = renderToStaticMarkup(<PageFormProvider>{element}</PageFormProvider>);

    expect(markup).toContain("Not set");
  });
});
