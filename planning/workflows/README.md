# HR Workflow Exploration Index

This directory incrementally models the workflows that an HR service may need.
It begins with concrete legacy workflow configurations, extracts their useful
behavior, identifies unsafe or incomplete behavior, and then expands outward to
adjacent HCM workflows.

Nothing in this directory is implementation evidence. Each workflow has one of
these discovery states:

```text
EXTRACTED       concrete legacy configuration exists
REFERENCE       target reference workflow exists
EXPLORED        steps, dependencies and data have been modeled
CONTRACTED      normative schemas and capability contracts resolve
IMPLEMENTED     current Go runtime and conformance evidence exist
```

## Current extraction set

| Domain    | Workflow                                                                 | Source                                                    | Discovery state      |
| --------- | ------------------------------------------------------------------------ | --------------------------------------------------------- | -------------------- |
| People    | [Contact information update](people/contact-information-update.md)       | `employee-contact-info-update.workflow.json`              | EXTRACTED + EXPLORED |
| People    | [Emergency contact update](people/emergency-contact-update.md)           | `employee-emergency-contact-update.workflow.json`         | EXTRACTED + EXPLORED |
| People    | [Legal name change](people/legal-name-change.md)                         | `employee-legal-name-change.workflow.json`                | EXTRACTED + EXPLORED |
| Rewards   | [Compensation change](rewards/compensation-change.md)                    | `employee-compensation-change.workflow.json`              | EXTRACTED + EXPLORED |
| Workforce | [Org transfer with compensation](workforce/org-transfer-compensation.md) | `employee-org-transfer-compensation-change.workflow.json` | EXTRACTED + EXPLORED |
| Workforce | [Headcount requisition](workforce/headcount-requisition.md)              | `position-headcount-requisition.workflow.json`            | EXTRACTED + EXPLORED |
| Lifecycle | [Termination and offboarding](lifecycle/termination-offboarding.md)      | `employee-termination.workflow.json` plus reference suite | EXTRACTED + EXPLORED |

## Target reference set

| Domain    | Workflow                                                            | Discovery state      |
| --------- | ------------------------------------------------------------------- | -------------------- |
| People    | [Manager change](people/manager-change.md)                          | REFERENCE + EXPLORED |
| Lifecycle | [Recruit, hire and onboard](lifecycle/recruit-hire-onboard.md)      | REFERENCE + EXPLORED |
| Rewards   | [Promotion into management](rewards/promotion-into-management.md)   | REFERENCE + EXPLORED |
| Leave     | [Leave and return to work](leave/leave-return-to-work.md)           | REFERENCE + EXPLORED |
| Payroll   | [Payroll correction and retro](payroll/payroll-correction-retro.md) | REFERENCE + EXPLORED |

The reusable modeling fields and dependency vocabulary are defined in the
[exploration contract](_shared/exploration-contract.md), with reusable complete
phase sequences in [workflow archetypes](_shared/workflow-archetypes.md). The
[discovery backlog](discovery-backlog.md) records the next workflow families and
prevents an intent name from being mistaken for a modeled workflow.

Adjacent intent catalogs modeled through those archetypes:

- [People and Employment](people/catalog.md)
- [Organization, Job, Position and Headcount](workforce/catalog.md)
- [Compensation and Rewards](rewards/catalog.md)
- [Recruiting, Onboarding and Offboarding](lifecycle/catalog.md)
- [Benefits, Time, Leave and Accommodation](leave/catalog.md)
- [HR Service Delivery and Employee Relations](hr-service/catalog.md)

These catalogs currently map 198 HR-domain intents: 39 People/Employment,
28 Organization/Job/Position, 21 Rewards, 41 Recruiting/Onboarding/Offboarding,
46 Benefits/Time/Leave, and 23 HR Service/Employee Relations workflows. A catalog
mapping is broader and shallower than an individual workflow deep dive.

The [HR Workflow Data and Property Register](hr-workflow-data-register.md)
consolidates recurring candidate entities, fields, execution evidence and
unresolved modeling decisions.

Workflow-engine exploration:

- [All 17 step-type contracts](_engine/step-types.md)
- [Step-type sample coverage matrix](_engine/step-type-coverage.md)
- [Normative workflow context contract](_engine/workflow-context-contract.md)
- [32-review adversarial workflow-context audit](_engine/workflow-context-adversarial-audit-2026-08-14.md)
- Gap-closing samples: [verified endpoint](samples/verified-contact-endpoint-change.md),
  [bulk policy acknowledgement](samples/bulk-policy-acknowledgement.md),
  [agent-assisted case triage](samples/agent-assisted-hr-case-triage.md), and
  [equipment shipment cancellation](samples/equipment-shipment-cancellation.md)
- Cross-cutting adversarial samples: [workflow context edge cases](samples/workflow-context-edge-cases.md)

## Common execution spine

```text
BusinessIntent
      |
      v
Resolve subject + source authority
      |
      v
Collect typed input and evidence
      |
      v
Validate + govern + conflict-check
      |
      v
Simulate immutable proposal
      |
      v
Resolve and collect approvals/tasks
      |
      v
Wait until effective time, if required
      |
      v
Execution-time revalidation
      |
      v
Atomic local commit
      |
      v
Dispatch ordered external effects
      |
      v
Observe + reconcile
      |
      +---- consistent ----> close
      |
      +---- drift/unknown --> diagnose + RepairPlan
```

Each workflow may omit a step only by declaring why it is not applicable. It may
not leave the responsibility implicit.

## Authority rule

Legacy TypeScript JSON is behavioral evidence only. Target workflows use Go,
Protobuf, SchemaFlux-generated definitions, governed capabilities, and the
durable workflow kernel. Where a source configuration says an external request
was merely queued or accepted, the exploratory target keeps external consistency
pending until observation and reconciliation complete.
