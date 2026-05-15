import type { ReactNode } from "react";
import type { StatusBadgeConfig, WidgetLayoutProps, WidgetStyleProps } from "./types";
import { stringValue, widgetClassName, widgetStyleVariables } from "./utils";

const statusClassName = (status: string): string => {
  const normalizedStatus = status.toLowerCase();

  if (normalizedStatus === "success" || normalizedStatus === "low") {
    return "status-badge status-success";
  }

  if (normalizedStatus === "warning" || normalizedStatus === "medium") {
    return "status-badge status-warning";
  }

  if (
    normalizedStatus === "danger" ||
    normalizedStatus === "error" ||
    normalizedStatus === "high"
  ) {
    return "status-badge status-error";
  }

  return "status-badge status-info";
};

export function StatusBadge({ status, label }: StatusBadgeConfig): JSX.Element {
  const badgeLabel = stringValue(label, status);

  return <span className={statusClassName(status)}>{badgeLabel}</span>;
}

export function WidgetRoot({
  children,
  className,
  styleProps,
}: {
  children: ReactNode;
  className: string;
  styleProps?: WidgetStyleProps | undefined;
}): JSX.Element {
  return (
    <div
      className={widgetClassName(className, styleProps)}
      style={widgetStyleVariables(styleProps)}
    >
      {children}
    </div>
  );
}

export function EmptyState({
  label,
  styleProps,
}: {
  label: string;
  styleProps?: WidgetStyleProps | undefined;
}): JSX.Element {
  return (
    <WidgetRoot className="content-block" styleProps={styleProps}>
      <p>{label}</p>
    </WidgetRoot>
  );
}

export function WidgetSection({
  children,
  title,
  description,
  ...styleProps
}: WidgetLayoutProps): JSX.Element {
  return (
    <section
      className={widgetClassName("widget-section-layout", styleProps)}
      style={widgetStyleVariables(styleProps)}
    >
      {title !== undefined || description !== undefined ? (
        <header className="widget-header">
          <div>
            {title !== undefined ? <h2>{title}</h2> : null}
            {description !== undefined ? <p>{description}</p> : null}
          </div>
        </header>
      ) : null}
      {children}
    </section>
  );
}

export function WidgetStack({
  children,
  ...styleProps
}: WidgetLayoutProps): JSX.Element {
  return (
    <div
      className={widgetClassName("widget-stack-layout", styleProps)}
      style={widgetStyleVariables(styleProps)}
    >
      {children}
    </div>
  );
}

export function WidgetGrid({
  children,
  ...styleProps
}: WidgetLayoutProps): JSX.Element {
  return (
    <div
      className={widgetClassName("widget-grid-layout", styleProps)}
      style={widgetStyleVariables(styleProps)}
    >
      {children}
    </div>
  );
}
