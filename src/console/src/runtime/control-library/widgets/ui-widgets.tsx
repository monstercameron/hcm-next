import { useState } from "react";
import type { WidgetComponentProps, WidgetRecord } from "./types";
import { EmptyState, StatusBadge, WidgetRoot } from "./primitives";
import { recordsValue, resolveWidgetStyleProps, stringValue } from "./utils";

export type TabItemConfig = {
  id: string;
  label: string;
  content?: string;
  status?: string;
};

export type TabsConfig = {
  tabs: readonly TabItemConfig[];
  selectedId?: string;
  emptyLabel?: string;
};

export type StepItemConfig = {
  id: string;
  label: string;
  detail?: string;
  status?: string;
};

export type StepperConfig = {
  steps: readonly StepItemConfig[];
  currentStepId?: string;
  emptyLabel?: string;
};

export type ModalDrawerConfig = {
  title: string;
  body?: string;
  openLabel?: string;
  closeLabel?: string;
  defaultOpen?: boolean;
  placement?: "modal" | "drawer";
};

export type ToastItemConfig = {
  id: string;
  title: string;
  detail?: string;
  status?: string;
};

export type ToastCenterConfig = {
  toasts: readonly ToastItemConfig[];
  emptyLabel?: string;
};

const optionalString = (value: unknown): string | undefined => {
  const text = stringValue(value);

  return text.length > 0 ? text : undefined;
};

export const tabItemsFromRecords = (
  records: readonly WidgetRecord[],
): readonly TabItemConfig[] =>
  records.map((record, index) => {
    const content = optionalString(record.content) ?? optionalString(record.body);
    const status = optionalString(record.status);

    return {
      id: stringValue(record.id, stringValue(record.label, `tab-${index + 1}`)),
      label: stringValue(record.label, `Tab ${index + 1}`),
      ...(content === undefined ? {} : { content }),
      ...(status === undefined ? {} : { status }),
    };
  });

export const stepItemsFromRecords = (
  records: readonly WidgetRecord[],
): readonly StepItemConfig[] =>
  records.map((record, index) => {
    const detail = optionalString(record.detail) ?? optionalString(record.description);
    const status = optionalString(record.status);

    return {
      id: stringValue(record.id, stringValue(record.label, `step-${index + 1}`)),
      label: stringValue(record.label, `Step ${index + 1}`),
      ...(detail === undefined ? {} : { detail }),
      ...(status === undefined ? {} : { status }),
    };
  });

export const toastItemsFromRecords = (
  records: readonly WidgetRecord[],
): readonly ToastItemConfig[] =>
  records.map((record, index) => {
    const title = stringValue(
      record.title,
      stringValue(record.label, `Toast ${index + 1}`),
    );
    const detail = optionalString(record.detail) ?? optionalString(record.description);

    return {
      id: stringValue(record.id, title),
      title,
      ...(detail === undefined ? {} : { detail }),
      status: stringValue(record.status, "info"),
    };
  });

export function TabsWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<TabsConfig>): JSX.Element {
  const firstTabId = config.tabs[0]?.id ?? "";
  const [activeTabId, setActiveTabId] = useState(config.selectedId ?? firstTabId);
  const activeTab = config.tabs.find((tab) => tab.id === activeTabId) ?? config.tabs[0];
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  if (config.tabs.length === 0) {
    return (
      <EmptyState
        label={stringValue(config.emptyLabel, "No tabs configured.")}
        styleProps={resolvedStyleProps}
      />
    );
  }

  return (
    <WidgetRoot className="tabs-widget" styleProps={resolvedStyleProps}>
      <div className="graph-toolbar" role="tablist" aria-label="Tabs">
        {config.tabs.map((tab) => (
          <button
            aria-selected={activeTab?.id === tab.id}
            className={activeTab?.id === tab.id ? "segment-active" : ""}
            key={tab.id}
            onClick={() => setActiveTabId(tab.id)}
            role="tab"
            type="button"
          >
            {tab.label}
          </button>
        ))}
      </div>
      <section role="tabpanel">
        <strong>{activeTab?.label}</strong>
        {activeTab?.status !== undefined ? (
          <StatusBadge status={activeTab.status} styleProps={resolvedStyleProps} />
        ) : null}
        {activeTab?.content !== undefined ? <p>{activeTab.content}</p> : null}
      </section>
    </WidgetRoot>
  );
}

export function StepperWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<StepperConfig>): JSX.Element {
  const firstStepId = config.steps[0]?.id ?? "";
  const [currentStepId, setCurrentStepId] = useState(
    config.currentStepId ?? firstStepId,
  );
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  if (config.steps.length === 0) {
    return (
      <EmptyState
        label={stringValue(config.emptyLabel, "No steps configured.")}
        styleProps={resolvedStyleProps}
      />
    );
  }

  return (
    <WidgetRoot className="stepper-widget" styleProps={resolvedStyleProps}>
      <ol className="timeline">
        {config.steps.map((step) => (
          <li key={step.id}>
            <button
              aria-current={currentStepId === step.id ? "step" : undefined}
              className={currentStepId === step.id ? "segment-active" : ""}
              onClick={() => setCurrentStepId(step.id)}
              type="button"
            >
              {step.label}
            </button>
            {step.detail !== undefined ? <small>{step.detail}</small> : null}
            {step.status !== undefined ? (
              <StatusBadge status={step.status} styleProps={resolvedStyleProps} />
            ) : null}
          </li>
        ))}
      </ol>
    </WidgetRoot>
  );
}

export function ModalDrawerWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<ModalDrawerConfig>): JSX.Element {
  const [open, setOpen] = useState(config.defaultOpen === true);
  const placement = config.placement ?? "modal";
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot
      className={`${placement}-drawer-widget`}
      styleProps={resolvedStyleProps}
    >
      <button onClick={() => setOpen(true)} type="button">
        {config.openLabel ?? config.title}
      </button>
      {open ? (
        <section aria-modal="true" role="dialog">
          <header className="widget-header">
            <h2>{config.title}</h2>
            <button onClick={() => setOpen(false)} type="button">
              {config.closeLabel ?? "Close"}
            </button>
          </header>
          {config.body !== undefined ? <p>{config.body}</p> : null}
        </section>
      ) : null}
    </WidgetRoot>
  );
}

export function ToastCenterWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<ToastCenterConfig>): JSX.Element {
  const [dismissedToastIds, setDismissedToastIds] = useState<readonly string[]>([]);
  const visibleToasts = config.toasts.filter(
    (toast) => !dismissedToastIds.includes(toast.id),
  );
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  if (visibleToasts.length === 0) {
    return (
      <EmptyState
        label={stringValue(config.emptyLabel, "No notifications.")}
        styleProps={resolvedStyleProps}
      />
    );
  }

  return (
    <WidgetRoot className="toast-center-widget" styleProps={resolvedStyleProps}>
      {visibleToasts.map((toast) => (
        <article className="check-row" key={toast.id}>
          <div>
            <strong>{toast.title}</strong>
            {toast.detail !== undefined ? <span>{toast.detail}</span> : null}
          </div>
          <StatusBadge
            status={toast.status ?? "info"}
            styleProps={resolvedStyleProps}
          />
          <button
            aria-label={`Dismiss ${toast.title}`}
            onClick={() => setDismissedToastIds((current) => [...current, toast.id])}
            type="button"
          >
            Dismiss
          </button>
        </article>
      ))}
    </WidgetRoot>
  );
}

export const tabsConfigFromRecords = (value: unknown): readonly TabItemConfig[] =>
  tabItemsFromRecords(recordsValue(value));

export const stepsConfigFromRecords = (value: unknown): readonly StepItemConfig[] =>
  stepItemsFromRecords(recordsValue(value));

export const toastsConfigFromRecords = (value: unknown): readonly ToastItemConfig[] =>
  toastItemsFromRecords(recordsValue(value));
