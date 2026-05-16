import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";

/**
 * Lightweight subject record the picker writes into the form context. Mirrors
 * the SubjectOption shape but adds the few HR facts the record-summary widget
 * needs to render without re-fetching.
 */
export type PageFormSubject = {
  id: string;
  displayName: string;
  jobTitle?: string;
  department?: string;
  manager?: string;
};

export type PageFormContextValue = {
  workflowIntent?: string;
  workflowSubjectType?: string;
  actorId?: string;
  availableSubjects: readonly PageFormSubject[];
  selectedSubjectId?: string;
  selectedSubject?: PageFormSubject;
  formValues: Record<string, unknown>;
  submitResult?: PageFormSubmitResult;
  setSelectedSubjectId: (id: string) => void;
  setFieldValue: (fieldId: string, value: unknown) => void;
  setAvailableSubjects: (subjects: readonly PageFormSubject[]) => void;
  setWorkflowIntent: (intent: string) => void;
  setWorkflowSubjectType: (subjectType: string) => void;
  setActorId: (actorId: string | undefined) => void;
  setSubmitResult: (result: PageFormSubmitResult | undefined) => void;
  resetForm: () => void;
};

/**
 * Confirmation payload the action bar renders after a successful POST to
 * /api/workflow-intents. Kept narrow so the rest of the page can pick out the
 * one or two fields it actually needs.
 */
export type PageFormSubmitResult = {
  workflowInstanceId: string;
  currentState: string;
  currentInteraction?: string;
};

const PageFormContext = createContext<PageFormContextValue | undefined>(undefined);

/**
 * Pure derivation used by the provider. Exposed so unit tests can verify the
 * lookup rule without booting a React renderer.
 */
export function deriveSelectedSubject(
  availableSubjects: readonly PageFormSubject[],
  selectedSubjectId: string | undefined,
): PageFormSubject | undefined {
  if (selectedSubjectId === undefined) {
    return undefined;
  }
  return availableSubjects.find((subject) => subject.id === selectedSubjectId);
}

export type PageFormProviderProps = {
  children: ReactNode;
  initialWorkflowIntent?: string;
  initialWorkflowSubjectType?: string;
  initialActorId?: string;
  initialAvailableSubjects?: readonly PageFormSubject[];
  initialSelectedSubjectId?: string;
};

/**
 * Provides shared form state for every widget rendered inside a single
 * `WorkflowPageRenderer`. The picker writes the active subject, fields write
 * their values, and the action bar reads everything back when it submits.
 */
export function PageFormProvider({
  children,
  initialWorkflowIntent,
  initialWorkflowSubjectType,
  initialActorId,
  initialAvailableSubjects,
  initialSelectedSubjectId,
}: PageFormProviderProps): JSX.Element {
  const [workflowIntent, setWorkflowIntent] = useState<string | undefined>(
    initialWorkflowIntent,
  );
  const [workflowSubjectType, setWorkflowSubjectType] = useState<string | undefined>(
    initialWorkflowSubjectType,
  );
  const [actorId, setActorId] = useState<string | undefined>(initialActorId);
  const [availableSubjects, setAvailableSubjects] = useState<
    readonly PageFormSubject[]
  >(initialAvailableSubjects ?? []);
  const [selectedSubjectId, setSelectedSubjectIdState] = useState<string | undefined>(
    initialSelectedSubjectId,
  );
  const [formValues, setFormValues] = useState<Record<string, unknown>>({});
  const [submitResult, setSubmitResult] = useState<PageFormSubmitResult | undefined>(
    undefined,
  );

  const setSelectedSubjectId = useCallback((id: string) => {
    setSelectedSubjectIdState(id.length === 0 ? undefined : id);
  }, []);

  const setFieldValue = useCallback((fieldId: string, value: unknown) => {
    setFormValues((current) => ({ ...current, [fieldId]: value }));
  }, []);

  const setAvailableSubjectsCallback = useCallback(
    (subjects: readonly PageFormSubject[]) => {
      setAvailableSubjects(subjects);
    },
    [],
  );

  const setWorkflowIntentCallback = useCallback((intent: string) => {
    setWorkflowIntent(intent);
  }, []);

  const setWorkflowSubjectTypeCallback = useCallback((subjectType: string) => {
    setWorkflowSubjectType(subjectType);
  }, []);

  const setActorIdCallback = useCallback((next: string | undefined) => {
    setActorId(next);
  }, []);

  const setSubmitResultCallback = useCallback(
    (next: PageFormSubmitResult | undefined) => {
      setSubmitResult(next);
    },
    [],
  );

  const resetForm = useCallback(() => {
    setFormValues({});
    setSelectedSubjectIdState(undefined);
    setSubmitResult(undefined);
  }, []);

  const selectedSubject = useMemo<PageFormSubject | undefined>(
    () => deriveSelectedSubject(availableSubjects, selectedSubjectId),
    [availableSubjects, selectedSubjectId],
  );

  const value = useMemo<PageFormContextValue>(() => {
    const base: PageFormContextValue = {
      availableSubjects,
      formValues,
      setSelectedSubjectId,
      setFieldValue,
      setAvailableSubjects: setAvailableSubjectsCallback,
      setWorkflowIntent: setWorkflowIntentCallback,
      setWorkflowSubjectType: setWorkflowSubjectTypeCallback,
      setActorId: setActorIdCallback,
      setSubmitResult: setSubmitResultCallback,
      resetForm,
    };
    if (workflowIntent !== undefined) {
      base.workflowIntent = workflowIntent;
    }
    if (workflowSubjectType !== undefined) {
      base.workflowSubjectType = workflowSubjectType;
    }
    if (actorId !== undefined) {
      base.actorId = actorId;
    }
    if (selectedSubjectId !== undefined) {
      base.selectedSubjectId = selectedSubjectId;
    }
    if (selectedSubject !== undefined) {
      base.selectedSubject = selectedSubject;
    }
    if (submitResult !== undefined) {
      base.submitResult = submitResult;
    }
    return base;
  }, [
    actorId,
    availableSubjects,
    formValues,
    resetForm,
    selectedSubject,
    selectedSubjectId,
    setActorIdCallback,
    setAvailableSubjectsCallback,
    setFieldValue,
    setSelectedSubjectId,
    setSubmitResultCallback,
    setWorkflowIntentCallback,
    setWorkflowSubjectTypeCallback,
    submitResult,
    workflowIntent,
    workflowSubjectType,
  ]);

  return <PageFormContext.Provider value={value}>{children}</PageFormContext.Provider>;
}

/**
 * Returns the active form context. Widgets that opt into form wiring should
 * call this hook; widgets rendered outside a provider get a safe inert value
 * so existing pages without the provider continue to render.
 */
export function usePageForm(): PageFormContextValue {
  const value = useContext(PageFormContext);
  if (value === undefined) {
    return INERT_PAGE_FORM_CONTEXT;
  }
  return value;
}

const noop = (): void => {};

const INERT_PAGE_FORM_CONTEXT: PageFormContextValue = {
  availableSubjects: [],
  formValues: {},
  setSelectedSubjectId: noop,
  setFieldValue: noop,
  setAvailableSubjects: noop,
  setWorkflowIntent: noop,
  setWorkflowSubjectType: noop,
  setActorId: noop,
  setSubmitResult: noop,
  resetForm: noop,
};
