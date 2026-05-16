import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { useEffect } from "react";
import {
  PageFormProvider,
  deriveSelectedSubject,
  usePageForm,
  type PageFormSubject,
} from "./page-form-context.js";

const subject = (id: string, displayName: string): PageFormSubject => ({
  id,
  displayName,
  jobTitle: `${displayName} title`,
  department: `${displayName} dept`,
  manager: `${displayName} manager`,
});

describe("deriveSelectedSubject", () => {
  const subjects = [subject("emp_1", "Leo Park"), subject("emp_2", "Ana Cruz")];

  it("returns undefined when no id is selected", () => {
    expect(deriveSelectedSubject(subjects, undefined)).toBeUndefined();
  });

  it("returns the matching subject when present", () => {
    expect(deriveSelectedSubject(subjects, "emp_2")?.displayName).toBe("Ana Cruz");
  });

  it("returns undefined when id is not in availableSubjects", () => {
    expect(deriveSelectedSubject(subjects, "missing")).toBeUndefined();
  });
});

describe("PageFormProvider", () => {
  it("exposes initial workflow intent and available subjects via usePageForm", () => {
    let captured: ReturnType<typeof usePageForm> | undefined;

    function Capture(): JSX.Element | null {
      const form = usePageForm();
      useEffect(() => {
        captured = form;
      }, [form]);
      // Render synchronously so the static markup captures the latest hook value.
      captured = form;
      return null;
    }

    renderToStaticMarkup(
      <PageFormProvider
        initialWorkflowIntent="employee.termination"
        initialAvailableSubjects={[subject("emp_1", "Leo Park")]}
      >
        <Capture />
      </PageFormProvider>,
    );

    expect(captured).toBeDefined();
    expect(captured?.workflowIntent).toBe("employee.termination");
    expect(captured?.availableSubjects).toHaveLength(1);
    expect(captured?.availableSubjects[0]?.displayName).toBe("Leo Park");
  });

  it("returns an inert context when no provider is mounted", () => {
    let captured: ReturnType<typeof usePageForm> | undefined;

    function Capture(): JSX.Element | null {
      captured = usePageForm();
      return null;
    }

    renderToStaticMarkup(<Capture />);

    expect(captured?.formValues).toEqual({});
    expect(captured?.availableSubjects).toEqual([]);
    expect(() => captured?.setFieldValue("x", 1)).not.toThrow();
  });
});
