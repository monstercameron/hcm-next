import { describe, expect, it } from "vitest";
import {
  listWorkflowBlockCatalogEntries,
  validateWorkflowBlockReference,
} from "./catalog.js";

describe("workflow block catalog", () => {
  it("registers the existing deterministic Go blocks", () => {
    const catalogEntries = listWorkflowBlockCatalogEntries();
    const blockNames = catalogEntries.map((entry) => {
      return entry.name;
    });

    expect(catalogEntries).toHaveLength(10);
    expect(blockNames).toEqual(
      expect.arrayContaining([
        "system.employee_data.legal_name.preflight",
        "system.employee_data.legal_name.plan_transaction",
        "system.employee_data.contact_info.preflight",
        "system.employee_data.contact_info.plan_transaction",
        "system.employee_data.emergency_contact.preflight",
        "system.employee_data.emergency_contact.plan_transaction",
        "system.employee_data.compensation.preflight",
        "system.employee_data.compensation.plan_transaction",
        "system.employee_data.org_transfer.preflight",
        "system.employee_data.org_transfer.plan_transaction",
      ]),
    );
  });

  it("validates known block names and versions", () => {
    const validationResult = validateWorkflowBlockReference({
      name: "system.employee_data.legal_name.preflight",
      version: "1.0.0",
      runtime: "go",
    });

    expect(validationResult.ok).toBe(true);
  });

  it("rejects unknown blocks", () => {
    const validationResult = validateWorkflowBlockReference({
      name: "customer.employee_data.custom.preflight",
      version: "1.0.0",
      runtime: "go",
    });

    expect(validationResult.ok).toBe(false);
  });
});
