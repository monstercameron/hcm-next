import type { ReactNode } from "react";
import type {
  StatusBadgeConfig,
  WidgetBrandingStyleProps,
  WidgetLayoutProps,
  WidgetStyleProps,
} from "./types";
import {
  resolveWidgetStyleProps,
  stringValue,
  widgetClassName,
  widgetStyleVariables,
} from "./utils";

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

export function StatusBadge({
  status,
  label,
  styleProps,
  brandingStyleProps,
}: StatusBadgeConfig & {
  styleProps?: WidgetStyleProps | undefined;
  brandingStyleProps?: WidgetStyleProps | undefined;
}): JSX.Element {
  const badgeLabel = stringValue(label, status);
  const baseClassName = statusClassName(status);
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <span
      className={widgetClassName(baseClassName, resolvedStyleProps)}
      style={widgetStyleVariables(resolvedStyleProps)}
    >
      {badgeLabel}
    </span>
  );
}

export function WidgetRoot({
  children,
  className,
  styleProps,
  brandingStyleProps,
}: {
  children: ReactNode;
  className: string;
} & WidgetBrandingStyleProps): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <div
      className={widgetClassName(className, resolvedStyleProps)}
      style={widgetStyleVariables(resolvedStyleProps)}
    >
      {children}
    </div>
  );
}

export function EmptyState({
  label,
  styleProps,
  brandingStyleProps,
}: {
  label: string;
} & WidgetBrandingStyleProps): JSX.Element {
  return (
    <WidgetRoot
      brandingStyleProps={brandingStyleProps}
      className="content-block"
      styleProps={styleProps}
    >
      <p>{label}</p>
    </WidgetRoot>
  );
}

export function SectionLayout({
  children,
  title,
  description,
  ...rawStyleProps
}: WidgetLayoutProps): JSX.Element {
  const styleProps = resolveWidgetStyleProps(rawStyleProps);

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

export const WidgetSection = SectionLayout;

export function StackLayout({
  children,
  ...rawStyleProps
}: WidgetLayoutProps): JSX.Element {
  const styleProps = resolveWidgetStyleProps(rawStyleProps);

  return (
    <div
      className={widgetClassName("widget-stack-layout", styleProps)}
      style={widgetStyleVariables(styleProps)}
    >
      {children}
    </div>
  );
}

export const WidgetStack = StackLayout;

export function GridLayout({
  children,
  ...rawStyleProps
}: WidgetLayoutProps): JSX.Element {
  const styleProps = resolveWidgetStyleProps(rawStyleProps);

  return (
    <div
      className={widgetClassName("widget-grid-layout", styleProps)}
      style={widgetStyleVariables(styleProps)}
    >
      {children}
    </div>
  );
}

export const WidgetGrid = GridLayout;
