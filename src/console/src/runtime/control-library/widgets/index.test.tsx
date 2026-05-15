import { renderToStaticMarkup } from "react-dom/server";
import type { CSSProperties } from "react";
import type { ResolvedWidget } from "@hcm-next/ui-runtime";
import { describe, expect, it } from "vitest";
import {
  RequestQueueWidget,
  getWidgetFactoryEntry,
  renderWidgetFromRegistry,
  widgetRegistry,
} from "./index";

const resolvedWidget = (type: string): ResolvedWidget => ({
  instance: {
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
      style: {
        tone: "info",
        variant: "subtle",
      },
    },
  },
  definition: undefined,
  bindings: {},
});

describe("control-library widget registry", () => {
  it("returns registry entries by widget type", () => {
    const entry = getWidgetFactoryEntry("queue.requestList", widgetRegistry);

    expect(entry?.componentName).toBe("RequestQueueWidget");
  });

  it("renders configured widgets from registry entries", () => {
    const element = renderWidgetFromRegistry(resolvedWidget("queue.requestList"));

    expect(element?.type).toBe(RequestQueueWidget);
  });

  it("injects branding style props into registry-rendered widgets", () => {
    const style = {
      "--ui-control-accent": "#0044cc",
    } as CSSProperties;
    const element = renderWidgetFromRegistry(
      resolvedWidget("queue.requestList"),
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

  it("renders parameterized direct components without workflow mutation handlers", () => {
    const markup = renderToStaticMarkup(
      <RequestQueueWidget
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
