import { describe, expect, it } from "vitest";
import { ensureUniqueFieldIds } from "./WorkflowPageRenderer.js";

describe("ensureUniqueFieldIds", () => {
  it("preserves distinct declared ids", () => {
    const result = ensureUniqueFieldIds([
      { id: "terminationType", type: "select", label: "Termination type" },
      { id: "effectiveAt", type: "date", label: "Effective date" },
      { id: "businessReason", type: "textarea", label: "Business reason" },
    ]);

    expect(result.map((field) => field.id)).toEqual([
      "terminationType",
      "effectiveAt",
      "businessReason",
    ]);
  });

  it("synthesizes ids from labels when id is missing", () => {
    const result = ensureUniqueFieldIds([
      { type: "text", label: "Effective date" },
      { type: "text", label: "Termination type" },
    ]);

    expect(result[0]?.id).toBe("effective_date_0");
    expect(result[1]?.id).toBe("termination_type_1");
  });

  it("disambiguates duplicate ids so each field gets a unique slot", () => {
    const result = ensureUniqueFieldIds([
      { id: "field", type: "text" },
      { id: "field", type: "text" },
      { id: "field", type: "text" },
    ]);

    const ids = result.map((field) => field.id);
    expect(new Set(ids).size).toBe(3);
    expect(ids[0]).toBe("field");
    expect(ids[1]).toBe("field_2");
    expect(ids[2]).toBe("field_3");
  });

  it("falls back to a positional id when both id and label are empty", () => {
    const result = ensureUniqueFieldIds([
      { type: "text" },
      { type: "text" },
    ]);

    expect(result[0]?.id).toBe("field_0");
    expect(result[1]?.id).toBe("field_1");
  });
});
