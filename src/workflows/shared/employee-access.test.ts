import { describe, expect, it } from "vitest";
import type {
  ActorRecord,
  EmployeeProjectionRecord,
  OrganizationRelationshipRecord,
  OrganizationUnitRecord,
  RoleBindingRecord,
  WorkerAssignmentRecord,
} from "@human-capital-management-suite/data-store";
import {
  MASKED_EMPLOYEE_FIELD_VALUE,
  canViewEmployee,
  evaluateEmployeeAccess,
  filterEmployeeProjectionForActor,
  filterWorkflowSummaryForActor,
  visibleEmployeeProjectionForActor,
  type EmployeeAccessEvaluationContext,
  type EmployeeAccessGrants,
} from "./employee-access.js";

describe("employee access helpers", () => {
  it("allows self-scoped profile access only for the actor's employee record", () => {
    const actor = actorFixture({ linkedWorkerId: "emp_001", roles: ["employee"] });
    const ownProjection = projectionFixture({ employeeId: "emp_001" });
    const otherProjection = projectionFixture({ employeeId: "emp_002" });
    const grants: EmployeeAccessGrants = [
      {
        roles: ["employee"],
        fieldGroups: ["profile", "contact"],
        scopes: [{ type: "self" }],
      },
    ];

    expect(canViewEmployee(actor, ownProjection.document, "profile", grants).ok).toBe(
      true,
    );
    expect(canViewEmployee(actor, otherProjection.document, "profile", grants).ok).toBe(
      false,
    );
  });

  it("allows direct-report access while masking ungranted compensation fields", () => {
    const actor = actorFixture({ linkedWorkerId: "mgr_001", roles: ["manager"] });
    const directReport = projectionFixture({
      employeeId: "emp_001",
      managerEmployeeId: "mgr_001",
      extraDocument: {
        compensation: {
          amount: 120000,
          currency: "USD",
          payFrequency: "annual",
          bonusTargetPercent: 10,
          effectiveDate: "2026-01-01",
        },
      },
    });
    const grants: EmployeeAccessGrants = [
      {
        roles: ["manager"],
        fieldGroups: ["profile", "job", "employment"],
        scopes: [{ type: "direct_reports" }],
      },
    ];

    const result = filterEmployeeProjectionForActor(actor, directReport, grants);

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }

    expect(result.value.document.person.displayName).toBe("Jane Doe");
    expect(result.value.document.compensation.currency).toBe(
      MASKED_EMPLOYEE_FIELD_VALUE,
    );
    expect(result.value.document.contact.personalEmail).toBeNull();
    expect(result.value.document.emergencyContacts).toEqual([]);
    expect(result.value.indexedFields["displayName"]).toBe("Jane Doe");
    expect(result.value.indexedFields["jobCode"]).toBe("SWE-3");
    expect(result.value.indexedFields["businessUnit"]).toBeUndefined();
    expect(result.value.indexedFields["managerEmployeeId"]).toBeUndefined();
  });

  it("supports manager-chain visibility from document metadata", () => {
    const actor = actorFixture({ linkedWorkerId: "mgr_002", roles: ["manager"] });
    const projection = projectionFixture({
      employeeId: "emp_001",
      extraDocument: {
        custom: {
          managerChainEmployeeIds: ["mgr_001", "mgr_002"],
        },
      },
    });
    const grants: EmployeeAccessGrants = [
      {
        roles: ["manager"],
        fieldGroups: ["profile"],
        scopes: [{ type: "manager_chain" }],
      },
    ];

    expect(visibleEmployeeProjectionForActor(actor, projection, grants)).toBe(true);
  });

  it("supports business-unit scoped field grants from indexed fields", () => {
    const actor = actorFixture({ roles: ["finance_admin"] });
    const projection = projectionFixture({
      employeeId: "emp_001",
      indexedFields: {
        businessUnit: "Commercial",
      },
      extraDocument: {
        compensation: {
          amount: 120000,
          currency: "USD",
          payFrequency: "annual",
          bonusTargetPercent: 10,
          effectiveDate: "2026-01-01",
        },
      },
    });
    const grants: EmployeeAccessGrants = [
      {
        roles: ["finance_admin"],
        fieldGroups: ["profile", "compensation"],
        scopes: [{ type: "business_unit", values: ["Commercial"] }],
      },
    ];

    const result = filterEmployeeProjectionForActor(actor, projection, grants);

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }

    expect(result.value.document.compensation).toEqual({
      amount: 120000,
      currency: "USD",
      payFrequency: "annual",
      bonusTargetPercent: 10,
      effectiveDate: "2026-01-01",
    });
    expect(result.value.document.contact.personalEmail).toBeNull();
  });

  it("allows explicit global access across all field groups", () => {
    const actor = actorFixture({ roles: ["hr_admin"] });
    const projection = projectionFixture({
      employeeId: "emp_001",
      extraDocument: {
        compensation: {
          amount: 120000,
          currency: "USD",
          payFrequency: "annual",
          bonusTargetPercent: 10,
          effectiveDate: "2026-01-01",
        },
      },
    });
    const grants: EmployeeAccessGrants = [
      {
        roles: ["hr_admin"],
        fieldGroups: "all",
        scopes: [{ type: "global" }],
      },
    ];

    const result = filterEmployeeProjectionForActor(actor, projection, grants);

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }

    expect(result.value.document.contact.personalEmail).toBe(
      "jane.personal@example.com",
    );
    expect(result.value.document.emergencyContacts).toHaveLength(1);
    expect(result.value.document.compensation).toEqual({
      amount: 120000,
      currency: "USD",
      payFrequency: "annual",
      bonusTargetPercent: 10,
      effectiveDate: "2026-01-01",
    });
  });

  it("resolves direct-report scope through active worker assignments", () => {
    const actor = actorFixture({
      linkedWorkerId: "mgr_source",
      roles: ["manager"],
    });
    const projection = projectionFixture({
      employeeId: "emp_001",
      managerEmployeeId: "mgr_projection",
    });
    const access = accessContextFixture({
      roleBindings: [
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "manager",
          scopeType: "direct_reports",
          fieldGroups: ["profile", "organization", "job"],
        }),
      ],
      workerAssignments: [
        workerAssignmentFixture({
          employeeId: "emp_001",
          workerAssignmentId: "wa_emp_001_primary_team",
          orgUnitId: "org_team_boston",
          assignmentType: "primary_team",
          managerEmployeeId: "mgr_source",
        }),
      ],
    });

    const decision = evaluateEmployeeAccess({
      actor,
      employeeDocument: projection.document,
      indexedFields: projection.indexedFields,
      fieldGroup: "profile",
      access,
    });

    expect(decision.decision).toBe("allow");
    expect(decision.roleBindingId).toBe("rb_manager");
    expect(decision.matchedWorkerAssignmentIds).toEqual(["wa_emp_001_primary_team"]);
  });

  it("does not grant destination-manager durable visibility from proposed assignments", () => {
    const actor = actorFixture({
      linkedWorkerId: "mgr_destination",
      roles: ["manager"],
    });
    const projection = projectionFixture({
      employeeId: "emp_001",
      managerEmployeeId: "mgr_source",
    });
    const access = accessContextFixture({
      roleBindings: [
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "manager",
          scopeType: "direct_reports",
          fieldGroups: ["profile"],
        }),
      ],
      workerAssignments: [
        workerAssignmentFixture({
          employeeId: "emp_001",
          workerAssignmentId: "wa_emp_001_current_team",
          orgUnitId: "org_team_boston",
          assignmentType: "primary_team",
          managerEmployeeId: "mgr_source",
        }),
      ],
      proposedWorkerAssignments: [
        workerAssignmentFixture({
          employeeId: "emp_001",
          workerAssignmentId: "wa_emp_001_target_team",
          orgUnitId: "org_team_cambridge",
          assignmentType: "primary_team",
          managerEmployeeId: "mgr_destination",
          status: "pending_approval",
        }),
      ],
      includeProposedAssignments: true,
    });

    const decision = evaluateEmployeeAccess({
      actor,
      employeeDocument: projection.document,
      indexedFields: projection.indexedFields,
      fieldGroup: "profile",
      access,
    });

    expect(decision.decision).toBe("deny");
  });

  it("resolves finance approval scope through proposed target cost center assignments", () => {
    const actor = actorFixture({ roles: ["finance_admin"] });
    const projection = projectionFixture({
      employeeId: "emp_001",
      indexedFields: {
        costCenter: "CC-BOS",
      },
    });
    const access = accessContextFixture({
      includeProposedAssignments: true,
      roleBindings: [
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "finance_admin",
          scopeType: "cost_center",
          scopeValue: "CC-CAM",
          fieldGroups: ["workflow"],
        }),
      ],
      workerAssignments: [
        workerAssignmentFixture({
          employeeId: "emp_001",
          workerAssignmentId: "wa_emp_001_current_cost_center",
          orgUnitId: "org_cc_bos",
          assignmentType: "cost_center",
          metadata: { costCenter: "CC-BOS" },
        }),
      ],
      proposedWorkerAssignments: [
        workerAssignmentFixture({
          employeeId: "emp_001",
          workerAssignmentId: "wa_emp_001_target_cost_center",
          orgUnitId: "org_cc_cam",
          assignmentType: "cost_center",
          status: "pending_approval",
          metadata: { costCenter: "CC-CAM" },
        }),
      ],
    });

    const durableDecision = evaluateEmployeeAccess({
      actor,
      employeeDocument: projection.document,
      indexedFields: projection.indexedFields,
      fieldGroup: "workflow",
      access: { ...access, includeProposedAssignments: false },
    });
    const approvalDecision = evaluateEmployeeAccess({
      actor,
      employeeDocument: projection.document,
      indexedFields: projection.indexedFields,
      fieldGroup: "workflow",
      access,
    });

    expect(durableDecision.decision).toBe("deny");
    expect(approvalDecision.decision).toBe("allow");
    expect(approvalDecision.matchedWorkerAssignmentIds).toEqual([
      "wa_emp_001_target_cost_center",
    ]);
  });

  it("resolves medical director clinical scope through org-unit descendants", () => {
    const actor = actorFixture({ roles: ["clinical_admin"] });
    const projection = projectionFixture({ employeeId: "emp_001" });
    const access = accessContextFixture({
      roleBindings: [
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "clinical_admin",
          scopeType: "org_unit_descendants",
          scopeOrgUnitId: "org_clinical",
          fieldGroups: ["profile", "organization", "job"],
        }),
      ],
      workerAssignments: [
        workerAssignmentFixture({
          employeeId: "emp_001",
          workerAssignmentId: "wa_emp_001_cambridge_nursing",
          orgUnitId: "org_team_cambridge_nursing",
          assignmentType: "primary_team",
        }),
      ],
      organizationRelationships: [
        organizationRelationshipFixture({
          fromOrgUnitId: "org_department_clinical_care",
          toOrgUnitId: "org_clinical",
        }),
        organizationRelationshipFixture({
          fromOrgUnitId: "org_team_cambridge_nursing",
          toOrgUnitId: "org_department_clinical_care",
        }),
      ],
    });

    const decision = evaluateEmployeeAccess({
      actor,
      employeeDocument: projection.document,
      indexedFields: projection.indexedFields,
      fieldGroup: "job",
      access,
    });

    expect(decision.decision).toBe("allow");
    expect(decision.matchedOrgUnitIds).toEqual(["org_team_cambridge_nursing"]);
  });

  it("does not let HR global scope see compensation unless the field group is granted", () => {
    const actor = actorFixture({ roles: ["hr_admin"] });
    const projection = projectionFixture({ employeeId: "emp_001" });
    const access = accessContextFixture({
      roleBindings: [
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "hr_admin",
          scopeType: "global",
          fieldGroups: [
            "profile",
            "organization",
            "job",
            "employment",
            "contact",
            "workflow",
          ],
        }),
      ],
    });

    expect(
      evaluateEmployeeAccess({
        actor,
        employeeDocument: projection.document,
        indexedFields: projection.indexedFields,
        fieldGroup: "profile",
        access,
      }).decision,
    ).toBe("allow");
    expect(
      evaluateEmployeeAccess({
        actor,
        employeeDocument: projection.document,
        indexedFields: projection.indexedFields,
        fieldGroup: "compensation",
        access,
      }).decision,
    ).toBe("deny");
  });

  it("masks contact fields and indexed contact values for compensation admins", () => {
    const actor = actorFixture({ roles: ["compensation_admin"] });
    const projection = projectionFixture({
      employeeId: "emp_001",
      indexedFields: {
        displayName: "Jane Doe",
        compensationAmount: 120000,
        personalEmail: "jane.personal@example.com",
        mobilePhone: "+15550001111",
        costCenter: "CC-CAM",
      },
    });
    const access = accessContextFixture({
      roleBindings: [
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "compensation_admin",
          scopeType: "global",
          fieldGroups: ["profile", "organization", "job", "compensation"],
        }),
      ],
    });

    const result = filterEmployeeProjectionForActor(actor, projection, access);

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }

    expect(result.value.document.contact.personalEmail).toBeNull();
    expect(result.value.document.contact.mobilePhone).toBeNull();
    expect(result.value.indexedFields["personalEmail"]).toBeUndefined();
    expect(result.value.indexedFields["mobilePhone"]).toBeUndefined();
    expect(result.value.indexedFields["compensationAmount"]).toBe(120000);
  });

  it("honors role-binding effective dates and revoked statuses", () => {
    const actor = actorFixture({ roles: ["hr_admin"] });
    const projection = projectionFixture({ employeeId: "emp_001" });
    const access = accessContextFixture({
      evaluatedAt: "2026-05-15T00:00:00.000Z",
      roleBindings: [
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "hr_admin",
          roleBindingId: "rb_expired",
          scopeType: "global",
          fieldGroups: ["profile"],
          effectiveStart: "2026-01-01T00:00:00.000Z",
          effectiveEnd: "2026-02-01T00:00:00.000Z",
        }),
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "hr_admin",
          roleBindingId: "rb_revoked",
          scopeType: "global",
          fieldGroups: ["profile"],
          status: "revoked",
        }),
      ],
    });

    const decision = evaluateEmployeeAccess({
      actor,
      employeeDocument: projection.document,
      indexedFields: projection.indexedFields,
      fieldGroup: "profile",
      access,
    });

    expect(decision.decision).toBe("deny");
  });

  it("lets explicit deny role bindings override matching allow bindings", () => {
    const actor = actorFixture({ roles: ["compensation_admin"] });
    const projection = projectionFixture({ employeeId: "emp_001" });
    const access = accessContextFixture({
      roleBindings: [
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "compensation_admin",
          roleBindingId: "rb_allow_compensation",
          scopeType: "global",
          fieldGroups: ["compensation"],
        }),
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "compensation_admin",
          roleBindingId: "rb_deny_compensation",
          scopeType: "global",
          fieldGroups: ["compensation"],
          metadata: { effect: "deny", fieldGroups: ["compensation"] },
        }),
      ],
    });

    const decision = evaluateEmployeeAccess({
      actor,
      employeeDocument: projection.document,
      indexedFields: projection.indexedFields,
      fieldGroup: "compensation",
      access,
    });

    expect(decision.decision).toBe("deny");
    expect(decision.roleBindingId).toBe("rb_deny_compensation");
  });

  it("exposes workflow restricted summaries without leaking ungranted fields", () => {
    const actor = actorFixture({ linkedWorkerId: "emp_001", roles: ["employee"] });
    const projection = projectionFixture({ employeeId: "emp_001" });
    const access = accessContextFixture({
      workflowInstanceId: "wfi_transfer_001",
      roleBindings: [
        roleBindingFixture({
          actorId: actor.actorId,
          roleKey: "employee",
          scopeType: "workflow_instance",
          scopeValue: "wfi_transfer_001",
          fieldGroups: ["workflow"],
        }),
      ],
    });
    const summary = {
      workflowInstanceId: "wfi_transfer_001",
      state: "waiting_destination_manager_approval",
      proposedOrganization: {
        team: "Cambridge Nursing",
        costCenter: "CC-CAM",
      },
      proposedCompensation: {
        amount: 98000,
        currency: "USD",
      },
      personalEmail: "jane.personal@example.com",
    };

    const result = filterWorkflowSummaryForActor({
      actor,
      projectionRecord: projection,
      workflowSummary: summary,
      access,
    });

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }

    expect(result.value["state"]).toBe("waiting_destination_manager_approval");
    expect(result.value["proposedOrganization"]).toBe(MASKED_EMPLOYEE_FIELD_VALUE);
    expect(result.value["proposedCompensation"]).toBe(MASKED_EMPLOYEE_FIELD_VALUE);
    expect(result.value["personalEmail"]).toBe(MASKED_EMPLOYEE_FIELD_VALUE);
  });
});

function actorFixture(input: {
  linkedWorkerId?: string;
  roles: string[];
}): ActorRecord {
  return {
    actorId: "actor_001",
    tenantId: "tenant_001",
    actorType: "human",
    linkedWorkerId: input.linkedWorkerId,
    email: "actor@example.com",
    displayName: "Test Actor",
    status: "active",
    roles: input.roles,
  };
}

function projectionFixture(input: {
  employeeId: string;
  managerEmployeeId?: string;
  indexedFields?: Record<string, unknown>;
  extraDocument?: Record<string, unknown>;
}): EmployeeProjectionRecord {
  return {
    tenantId: "tenant_001",
    employeeId: input.employeeId,
    projectionVersion: 1,
    document: {
      employeeId: input.employeeId,
      person: {
        personId: `person_${input.employeeId}`,
        legalName: {
          first: "Jane",
          middle: null,
          last: "Doe",
        },
        displayName: "Jane Doe",
        preferredName: null,
        workEmail: "jane.doe@example.com",
      },
      contact: {
        personalEmail: "jane.personal@example.com",
        mobilePhone: "+15550001111",
        homeAddress: {
          line1: "100 Market St",
          line2: null,
          city: "San Francisco",
          region: "CA",
          postalCode: "94105",
          country: "US",
        },
      },
      employment: {
        status: "active",
        legalEntity: "US-001",
        hireDate: "2024-01-01",
        workerType: "employee",
      },
      organization: {
        legalEntity: "US-001",
        businessUnit: "Engineering",
        department: "Platform",
        team: "Core Workflow",
        location: "San Francisco",
        payZone: "US-CA",
        costCenter: "ENG-100",
      },
      manager: {
        employeeId: input.managerEmployeeId ?? "mgr_000",
      },
      job: {
        jobCode: "SWE-3",
        title: "Software Engineer",
        family: "Engineering",
        level: "P3",
      },
      compensation: {
        amount: 100000,
        currency: "USD",
        payFrequency: "annual",
        bonusTargetPercent: 10,
        effectiveDate: "2026-01-01",
      },
      emergencyContacts: [
        {
          contactId: "ec_001",
          name: "Alex Doe",
          relationship: "spouse",
          phone: "+15551234567",
          email: "alex.doe@example.com",
          priority: 1,
        },
      ],
      custom: {},
      ...input.extraDocument,
    },
    indexedFields: input.indexedFields ?? {
      displayName: "Jane Doe",
      employmentStatus: "active",
      legalEntity: "US-001",
      businessUnit: "Engineering",
      department: "Platform",
      team: "Core Workflow",
      location: "San Francisco",
      costCenter: "ENG-100",
      managerEmployeeId: input.managerEmployeeId ?? "mgr_000",
      jobCode: "SWE-3",
      jobLevel: "P3",
    },
    createdAt: "2026-05-15T00:00:00.000Z",
    updatedAt: "2026-05-15T00:00:00.000Z",
  };
}

function accessContextFixture(input: {
  legacyGrants?: EmployeeAccessGrants;
  roleBindings?: RoleBindingRecord[];
  workerAssignments?: WorkerAssignmentRecord[];
  proposedWorkerAssignments?: WorkerAssignmentRecord[];
  organizationRelationships?: OrganizationRelationshipRecord[];
  organizationUnits?: OrganizationUnitRecord[];
  workflowInstanceId?: string;
  evaluatedAt?: string;
  includeProposedAssignments?: boolean;
}): EmployeeAccessEvaluationContext {
  return {
    legacyGrants: input.legacyGrants ?? [],
    roleBindings: input.roleBindings ?? [],
    workerAssignments: input.workerAssignments ?? [],
    proposedWorkerAssignments: input.proposedWorkerAssignments ?? [],
    organizationRelationships: input.organizationRelationships ?? [],
    organizationUnits: input.organizationUnits ?? [],
    ...(input.workflowInstanceId !== undefined
      ? { workflowInstanceId: input.workflowInstanceId }
      : {}),
    evaluatedAt: input.evaluatedAt ?? "2026-05-15T00:00:00.000Z",
    ...(input.includeProposedAssignments !== undefined
      ? { includeProposedAssignments: input.includeProposedAssignments }
      : {}),
  };
}

function roleBindingFixture(input: {
  actorId: string;
  roleKey: string;
  roleBindingId?: string;
  scopeType: RoleBindingRecord["scopeType"];
  scopeOrgUnitId?: string;
  scopeValue?: string;
  fieldGroups: string[];
  status?: RoleBindingRecord["status"];
  effectiveStart?: string;
  effectiveEnd?: string;
  metadata?: Record<string, unknown>;
}): RoleBindingRecord {
  return {
    roleBindingId: input.roleBindingId ?? `rb_${input.roleKey}`,
    tenantId: "tenant_001",
    actorId: input.actorId,
    roleKey: input.roleKey,
    scopeType: input.scopeType,
    ...(input.scopeOrgUnitId !== undefined
      ? { scopeOrgUnitId: input.scopeOrgUnitId }
      : {}),
    ...(input.scopeValue !== undefined ? { scopeValue: input.scopeValue } : {}),
    status: input.status ?? "active",
    effectiveStart: input.effectiveStart ?? "2026-01-01T00:00:00.000Z",
    ...(input.effectiveEnd !== undefined ? { effectiveEnd: input.effectiveEnd } : {}),
    metadata: input.metadata ?? {
      fieldGroups: input.fieldGroups,
    },
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  };
}

function workerAssignmentFixture(input: {
  employeeId: string;
  workerAssignmentId: string;
  orgUnitId: string;
  assignmentType: WorkerAssignmentRecord["assignmentType"];
  managerEmployeeId?: string;
  status?: WorkerAssignmentRecord["status"];
  effectiveStart?: string;
  effectiveEnd?: string;
  metadata?: Record<string, unknown>;
}): WorkerAssignmentRecord {
  return {
    workerAssignmentId: input.workerAssignmentId,
    tenantId: "tenant_001",
    employeeId: input.employeeId,
    orgUnitId: input.orgUnitId,
    assignmentType: input.assignmentType,
    ...(input.managerEmployeeId !== undefined
      ? { managerEmployeeId: input.managerEmployeeId }
      : {}),
    allocationPercent: 100,
    status: input.status ?? "active",
    effectiveStart: input.effectiveStart ?? "2026-01-01T00:00:00.000Z",
    ...(input.effectiveEnd !== undefined ? { effectiveEnd: input.effectiveEnd } : {}),
    metadata: input.metadata ?? {},
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  };
}

function organizationRelationshipFixture(input: {
  fromOrgUnitId: string;
  toOrgUnitId: string;
}): OrganizationRelationshipRecord {
  return {
    organizationRelationshipId: `orgrel_${input.fromOrgUnitId}_${input.toOrgUnitId}`,
    tenantId: "tenant_001",
    fromOrgUnitId: input.fromOrgUnitId,
    toOrgUnitId: input.toOrgUnitId,
    relationshipType: "part_of",
    status: "active",
    effectiveStart: "2026-01-01T00:00:00.000Z",
    metadata: {},
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  };
}
