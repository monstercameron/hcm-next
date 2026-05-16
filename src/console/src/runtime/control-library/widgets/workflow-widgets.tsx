import { useState } from "react";
import { CheckCircle2, CircleAlert, CircleHelp, CircleX } from "lucide-react";
import type { LabelValueItem, WidgetComponentProps, WidgetStyleProps } from "./types";
import { StatusBadge, WidgetRoot } from "./primitives";
import {
  resolveWidgetStyleProps,
  stringValue,
  valueToText,
  widgetClassName,
  widgetStyleVariables,
} from "./utils";

export type RequestQueueItem = {
  id: string;
  title: string;
  employee: string;
  state?: string;
  risk?: string;
  due?: string;
  detail?: string;
};

export type RequestQueueConfig = {
  requests: readonly RequestQueueItem[];
  emptyLabel?: string;
};

export type QueueListItem = RequestQueueItem;

export type QueueListConfig = RequestQueueConfig;

export type EmployeeSummaryConfig = {
  employee: {
    displayName: string;
    jobTitle?: string;
    department?: string;
    manager?: string;
    avatarLabel?: string;
  };
  facts?: readonly LabelValueItem[];
};

export type RecordSummaryConfig = EmployeeSummaryConfig;

export type ChangeDiffRow = {
  id: string;
  label: string;
  current: unknown;
  proposed: unknown;
  status?: string;
};

export type ChangeDiffConfig = {
  rows: readonly ChangeDiffRow[];
  emptyLabel?: string;
};

export type DiffViewerConfig = ChangeDiffConfig;

export type ApprovalActionConfig = {
  action: string;
  label: string;
  variant?: "primary" | "secondary" | "danger";
  disabled?: boolean;
  reason?: string;
};

export type ApprovalDecisionPanelConfig = {
  actions: readonly ApprovalActionConfig[];
  selectedAction?: string;
  description?: string;
  selectedLabel?: string;
};

export type ActionBarConfig = ApprovalDecisionPanelConfig;

export type ReasonCaptureConfig = {
  label?: string;
  placeholder?: string;
  value?: string;
  helperText?: string;
  required?: boolean;
  reasons?: readonly string[];
};

export type SimulationCheckConfig = {
  id: string;
  label: string;
  detail?: string;
  status?: string;
};

export type SimulationResultPanelConfig = {
  checks: readonly SimulationCheckConfig[];
  emptyLabel?: string;
};

export type ChecklistItemConfig = SimulationCheckConfig;

export type ChecklistConfig = SimulationResultPanelConfig;

export type TimelineEventConfig = {
  id: string;
  at: string;
  label: string;
  actor?: string;
  detail?: string;
  status?: string;
};

export type AuditTimelineConfig = {
  events: readonly TimelineEventConfig[];
  emptyLabel?: string;
};

export type TimelineConfig = AuditTimelineConfig;

const simulationIcon = (status: string): typeof CheckCircle2 => {
  if (status === "success") {
    return CheckCircle2;
  }

  if (status === "warning") {
    return CircleAlert;
  }

  if (status === "error" || status === "danger") {
    return CircleX;
  }

  return CircleHelp;
};

const renderEmptyIfNeeded = (
  count: number,
  emptyLabel: string | undefined,
  styleProps: WidgetStyleProps | undefined,
): JSX.Element | undefined =>
  count === 0 ? (
    <WidgetRoot className="content-block" styleProps={styleProps}>
      <p>{stringValue(emptyLabel, "No records configured.")}</p>
    </WidgetRoot>
  ) : undefined;

/** Displays workflow requests without mutating workflow state. */
export function RequestQueueWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<RequestQueueConfig>): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });
  const emptyState = renderEmptyIfNeeded(
    config.requests.length,
    config.emptyLabel,
    resolvedStyleProps,
  );

  if (emptyState !== undefined) {
    return emptyState;
  }

  return (
    <WidgetRoot className="queue-list" styleProps={resolvedStyleProps}>
      {config.requests.map((request) => (
        <article className="queue-row" key={request.id}>
          <div>
            <strong>{request.title}</strong>
            <span>{request.employee}</span>
            {request.detail !== undefined ? <small>{request.detail}</small> : null}
          </div>
          <div className="queue-row-meta">
            <StatusBadge
              status={request.risk ?? "info"}
              styleProps={resolvedStyleProps}
            />
            {request.state !== undefined ? <span>{request.state}</span> : null}
            {request.due !== undefined ? <small>{request.due}</small> : null}
          </div>
        </article>
      ))}
    </WidgetRoot>
  );
}

export const QueueListWidget = RequestQueueWidget;

/** Presents permission-filtered employee context from already-resolved data. */
export function EmployeeSummaryWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<EmployeeSummaryConfig>): JSX.Element {
  const employee = config.employee;
  const avatarLabel = stringValue(
    employee.avatarLabel,
    employee.displayName.slice(0, 2).toUpperCase(),
  );
  const facts = config.facts ?? [
    { label: "Role", value: employee.jobTitle },
    { label: "Department", value: employee.department },
    { label: "Manager", value: employee.manager },
  ];
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot className="employee-summary" styleProps={resolvedStyleProps}>
      <div className="avatar" aria-hidden="true">
        {avatarLabel}
      </div>
      <div className="summary-grid">
        <div>
          <span>Name</span>
          <strong>{employee.displayName}</strong>
        </div>
        {facts.map((fact) => (
          <div key={fact.label}>
            <span>{fact.label}</span>
            <strong>{valueToText(fact.value)}</strong>
          </div>
        ))}
      </div>
    </WidgetRoot>
  );
}

export const RecordSummaryWidget = EmployeeSummaryWidget;

/** Renders a deterministic current/proposed diff from supplied rows. */
export function ChangeDiffWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<ChangeDiffConfig>): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });
  const emptyState = renderEmptyIfNeeded(
    config.rows.length,
    config.emptyLabel,
    resolvedStyleProps,
  );

  if (emptyState !== undefined) {
    return emptyState;
  }

  return (
    <WidgetRoot className="diff-table" styleProps={resolvedStyleProps}>
      <div className="diff-row diff-heading" role="row">
        <span>Field</span>
        <span>Current</span>
        <span>Proposed</span>
      </div>
      {config.rows.map((row) => (
        <div className="diff-row" role="row" key={row.id}>
          <span>{row.label}</span>
          <span>{valueToText(row.current)}</span>
          <span>{valueToText(row.proposed)}</span>
          {row.status !== undefined ? (
            <StatusBadge status={row.status} styleProps={resolvedStyleProps} />
          ) : null}
        </div>
      ))}
    </WidgetRoot>
  );
}

export const DiffViewerWidget = ChangeDiffWidget;

/** Provides local decision preview controls; server transitions remain external. */
export function ApprovalDecisionPanelWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<ApprovalDecisionPanelConfig>): JSX.Element {
  const initialAction = config.selectedAction ?? config.actions[0]?.action ?? "";
  const [selectedAction, setSelectedAction] = useState(initialAction);
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot className="decision-panel" styleProps={resolvedStyleProps}>
      {config.description !== undefined ? <p>{config.description}</p> : null}
      <div className="button-row">
        {config.actions.map((action) => (
          <button
            aria-pressed={selectedAction === action.action}
            className={`action-button action-${action.variant ?? "secondary"} ${
              selectedAction === action.action ? "action-selected" : ""
            }`}
            disabled={action.disabled}
            key={action.action}
            onClick={() => setSelectedAction(action.action)}
            title={action.reason}
            type="button"
          >
            {action.label}
          </button>
        ))}
      </div>
      <div className="selected-pill">
        {config.selectedLabel ?? "Selected transition"}:{" "}
        {selectedAction.length > 0 ? selectedAction : "None"}
      </div>
    </WidgetRoot>
  );
}

export const ActionBarWidget = ApprovalDecisionPanelWidget;

/** Captures a local reason string for workflows that require explanation. */
export function ReasonCaptureWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<ReasonCaptureConfig>): JSX.Element {
  const [reason, setReason] = useState(config.value ?? "");
  const [presetReason, setPresetReason] = useState(config.reasons?.[0] ?? "");
  const label = config.label ?? "Reason";
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });

  return (
    <WidgetRoot className="reason-capture" styleProps={resolvedStyleProps}>
      <label>
        <span>
          {label}
          {config.required === true ? " *" : ""}
        </span>
        {config.reasons !== undefined && config.reasons.length > 0 ? (
          <select
            aria-label={`${label} preset`}
            onChange={(event) => setPresetReason(event.currentTarget.value)}
            value={presetReason}
          >
            {config.reasons.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        ) : null}
        <textarea
          aria-label={label}
          onChange={(event) => setReason(event.currentTarget.value)}
          placeholder={config.placeholder}
          required={config.required}
          value={reason}
        />
      </label>
      <div className="selected-pill">
        {label}: {reason.length > 0 ? reason : presetReason || "Not provided"}
      </div>
      {config.helperText !== undefined ? <small>{config.helperText}</small> : null}
    </WidgetRoot>
  );
}

/** Shows transaction preflight or simulation checks from supplied data. */
export function SimulationResultPanelWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<SimulationResultPanelConfig>): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });
  const emptyState = renderEmptyIfNeeded(
    config.checks.length,
    config.emptyLabel,
    resolvedStyleProps,
  );

  if (emptyState !== undefined) {
    return emptyState;
  }

  return (
    <WidgetRoot className="check-list" styleProps={resolvedStyleProps}>
      {config.checks.map((check) => {
        const status = check.status ?? "info";
        const Icon = simulationIcon(status);

        return (
          <article className="check-row" key={check.id}>
            <Icon size={20} aria-hidden />
            <div>
              <strong>{check.label}</strong>
              {check.detail !== undefined ? <span>{check.detail}</span> : null}
            </div>
            <StatusBadge status={status} styleProps={resolvedStyleProps} />
          </article>
        );
      })}
    </WidgetRoot>
  );
}

export const ChecklistWidget = SimulationResultPanelWidget;

/** Displays ledger-derived events; callers own data filtering and ordering. */
export function AuditTimelineWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<AuditTimelineConfig>): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });
  const emptyState = renderEmptyIfNeeded(
    config.events.length,
    config.emptyLabel,
    resolvedStyleProps,
  );

  if (emptyState !== undefined) {
    return emptyState;
  }

  return (
    <ol
      className={widgetClassName("timeline", resolvedStyleProps)}
      style={widgetStyleVariables(resolvedStyleProps)}
    >
      {config.events.map((event) => (
        <li key={event.id}>
          <time>{event.at}</time>
          <strong>{event.label}</strong>
          {event.actor !== undefined ? <span>{event.actor}</span> : null}
          {event.detail !== undefined ? <small>{event.detail}</small> : null}
          {event.status !== undefined ? (
            <StatusBadge status={event.status} styleProps={resolvedStyleProps} />
          ) : null}
        </li>
      ))}
    </ol>
  );
}

export const TimelineWidget = AuditTimelineWidget;
