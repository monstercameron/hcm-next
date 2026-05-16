import { useEffect, useId, useRef, useState } from "react";
import type { WidgetComponentProps } from "./types";
import { WidgetRoot } from "./primitives";
import { resolveWidgetStyleProps } from "./utils";
import { usePageForm } from "../../page-form-context.js";

/**
 * Employee option rendered inside the subject picker / list. Mirrors the
 * subset of `EmployeeProjectionRecord` the AI receives so the widget remains
 * free of data-store coupling.
 */
export type SubjectOption = {
  id: string;
  displayName: string;
  jobTitle?: string;
  department?: string;
  manager?: string;
};

export type SubjectPickerConfig = {
  label: string;
  helperText?: string;
  required: boolean;
  defaultSubjectId?: string;
  options: readonly SubjectOption[];
};

export type EmployeeListConfig = {
  label?: string;
  helperText?: string;
  options: readonly SubjectOption[];
  emptyLabel?: string;
};

const SUBJECT_PICKER_EVENT = "hcm:subject-picker-change";

function describeOption(option: SubjectOption): string {
  const parts = [option.jobTitle, option.department].filter(
    (part): part is string => typeof part === "string" && part.length > 0,
  );
  if (parts.length === 0) {
    return option.displayName;
  }
  return `${option.displayName} — ${parts.join(" • ")}`;
}

/**
 * Renders a labeled single-select combobox of employees. v1 just emits a
 * window-level CustomEvent on change so future v2 wiring (form submission
 * binding) can subscribe without a registry change. The picker uses the same
 * field-control styling as the rest of the form so it visually belongs to
 * whichever generated page hosts it.
 */
export function SubjectPickerWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<SubjectPickerConfig>): JSX.Element {
  const initial =
    config.defaultSubjectId !== undefined &&
    config.options.some((option) => option.id === config.defaultSubjectId)
      ? config.defaultSubjectId
      : "";
  const [selectedId, setSelectedId] = useState<string>(initial);
  const generatedId = useId();
  const selectId = `subject-picker-${generatedId}`;
  const helperId = config.helperText === undefined ? undefined : `${selectId}-helper`;
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });
  const pageForm = usePageForm();
  const syncedOptionsRef = useRef<readonly SubjectOption[] | undefined>(undefined);
  const syncedDefaultRef = useRef<string | undefined>(undefined);

  // Push the picker's options up to PageFormContext so sibling widgets (record
  // summary, action bar) can resolve the selected employee's facts without
  // calling back into the picker. Only resync when the option array reference
  // changes — config objects are stable across renders thanks to the registry.
  useEffect(() => {
    if (syncedOptionsRef.current === config.options) {
      return;
    }
    syncedOptionsRef.current = config.options;
    pageForm.setAvailableSubjects(config.options);
  }, [config.options, pageForm]);

  // If the page declared a default subject id (the AI pre-selected one), make
  // sure context reflects it on first render so the summary widget can render
  // before the user touches anything.
  useEffect(() => {
    if (syncedDefaultRef.current === initial) {
      return;
    }
    syncedDefaultRef.current = initial;
    if (initial.length > 0) {
      pageForm.setSelectedSubjectId(initial);
    }
  }, [initial, pageForm]);

  const onChange = (nextId: string): void => {
    setSelectedId(nextId);
    pageForm.setSelectedSubjectId(nextId);
    if (typeof window !== "undefined") {
      window.dispatchEvent(
        new CustomEvent(SUBJECT_PICKER_EVENT, {
          detail: { subjectId: nextId },
        }),
      );
    }
  };

  return (
    <WidgetRoot
      className="subject-picker-widget field-control"
      styleProps={resolvedStyleProps}
    >
      <label htmlFor={selectId}>
        <span>{config.label}</span>
        {config.required ? <em>required</em> : null}
      </label>
      <select
        aria-describedby={helperId}
        aria-required={config.required}
        id={selectId}
        onChange={(event) => onChange(event.currentTarget.value)}
        required={config.required}
        value={selectedId}
      >
        <option value="">Select an employee…</option>
        {config.options.map((option) => (
          <option key={option.id} value={option.id}>
            {describeOption(option)}
          </option>
        ))}
      </select>
      {config.helperText !== undefined ? (
        <small id={helperId}>{config.helperText}</small>
      ) : null}
    </WidgetRoot>
  );
}

/**
 * Renders a compact, hover/focus-styled list of employees as a lightweight
 * alternative to the picker. v1 is read-only — rows are buttons so the
 * existing keyboard focus styles apply, but they do not commit a selection.
 */
export function EmployeeListWidget({
  config,
  styleProps,
  brandingStyleProps,
}: WidgetComponentProps<EmployeeListConfig>): JSX.Element {
  const resolvedStyleProps = resolveWidgetStyleProps({
    brandingStyleProps,
    styleProps,
  });
  const emptyLabel = config.emptyLabel ?? "No employees to show.";

  return (
    <WidgetRoot className="employee-list-widget" styleProps={resolvedStyleProps}>
      {config.label !== undefined ? (
        <header className="employee-list-header">
          <strong>{config.label}</strong>
          {config.helperText !== undefined ? <small>{config.helperText}</small> : null}
        </header>
      ) : null}
      {config.options.length === 0 ? (
        <p className="employee-list-empty">{emptyLabel}</p>
      ) : (
        <ul className="employee-list-items">
          {config.options.map((option) => (
            <li key={option.id}>
              <button className="employee-list-row" tabIndex={0} type="button">
                <strong>{option.displayName}</strong>
                {option.jobTitle !== undefined || option.department !== undefined ? (
                  <small>
                    {[option.jobTitle, option.department]
                      .filter(
                        (part): part is string =>
                          typeof part === "string" && part.length > 0,
                      )
                      .join(" • ")}
                  </small>
                ) : null}
              </button>
            </li>
          ))}
        </ul>
      )}
    </WidgetRoot>
  );
}
