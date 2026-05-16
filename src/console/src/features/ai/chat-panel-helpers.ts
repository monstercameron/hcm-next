/**
 * Definition of a single quick-action chip rendered in the panel header. Chips
 * pre-fill the textarea with a templated starter sentence — the agent decides
 * what to do next once the user actually submits.
 */
export type QuickAction = {
  /** Stable identifier; used as the React key. */
  id: string;
  /** Visible chip label. */
  label: string;
  /** Workflow intent the chip describes (used only for the placeholder copy). */
  workflowIntent: string;
  /** Prompt body with `{subjectName}` etc. placeholders. */
  promptTemplate: string;
  /** Optional default subject id surfaced in templating. */
  defaultSubjectId?: string;
  /** Optional default subject display name used in substitution. */
  defaultSubjectName?: string;
};

/**
 * Canonical set of HR-friendly quick actions. Chips are surfaced only when
 * their `workflowIntent` is listed in `availableIntents` on the panel props.
 */
export const defaultQuickActions: readonly QuickAction[] = [
  {
    id: "start_termination",
    label: "Start a termination",
    workflowIntent: "employee.termination",
    promptTemplate: "Start a termination for {subjectName}",
  },
  {
    id: "update_contact_info",
    label: "Update contact info",
    workflowIntent: "employee.contact_update",
    promptTemplate: "Update contact info for {subjectName}",
  },
  {
    id: "approve_request",
    label: "Approve a request",
    workflowIntent: "employee.org_transfer_compensation_change",
    promptTemplate: "Help me approve the pending request for {subjectName}",
  },
];

const SUBJECT_NAME_FALLBACK = "this employee";

/**
 * Substitute the supported placeholder tokens (`{subjectName}` for now) in a
 * prompt template. Missing values fall back to the friendly "this employee"
 * stand-in so prompts always read as a complete sentence.
 */
export function renderPromptTemplate(
  template: string,
  args: { subjectName?: string },
): string {
  const subjectName =
    args.subjectName !== undefined && args.subjectName.length > 0
      ? args.subjectName
      : SUBJECT_NAME_FALLBACK;
  return template.replaceAll("{subjectName}", subjectName);
}

/**
 * Filter a candidate chip list to those whose intent is currently enabled.
 * Falls back to {@link defaultQuickActions} when no override is supplied.
 */
export function resolveAvailableChips(
  propActions: readonly QuickAction[] | undefined,
  availableIntents: readonly string[],
): readonly QuickAction[] {
  const source = propActions ?? defaultQuickActions;
  const intents = new Set(availableIntents);
  return source.filter((chip) => intents.has(chip.workflowIntent));
}
