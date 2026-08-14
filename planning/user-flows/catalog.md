# Initial User-Flow Catalog

This is the seed catalog for experience design and later todo/TDD generation.
Every entry inherits the complete [UserFlowRecord](README.md#userflowrecord) and
[MAX-v1](../workflows/vertical-slices/maximal-configuration-profile.md).

| Flow     | Primary participant/job                            | Root BusinessIntent examples                      | Archetype                 | Primary surfaces                            | Maturity/reference                                    |
| -------- | -------------------------------------------------- | ------------------------------------------------- | ------------------------- | ------------------------------------------- | ----------------------------------------------------- |
| `UF-001` | Any participant: discover a permitted action       | any discoverable intent                           | `UF-A1/A2/A3/A5/A7/A8`    | action discovery, context panel             | `FLOW_DRAFT`                                          |
| `UF-002` | Any initiator: create, save and resume a draft     | any request/change/process                        | `UF-A1/A2/A3`             | guided form, Draft Center                   | `FLOW_DRAFT`                                          |
| `UF-003` | Initiator: simulate, compare and submit a proposal | promotion, compensation, transfer, repair         | `UF-A2/A6`                | simulation compare, confirmation            | `FLOW_DRAFT`                                          |
| `UF-004` | Reviewer: make a permitted decision                | ApproveProposal, RejectProposal, evidence review  | `UF-A4`                   | Approval Inbox, task detail                 | `FLOW_DRAFT`                                          |
| `UF-005` | Participant: track a long-running intent           | any workflow/process/case                         | all                       | timeline, status summary, secure inbox      | `FLOW_DRAFT`                                          |
| `UF-006` | Participant: cancel, correct, supersede or appeal  | lifecycle child intents                           | all                       | intent inspector, confirmation              | `FLOW_DRAFT`                                          |
| `UF-007` | Manager: promote worker into management            | PromoteWorker plus child changes                  | `UF-A2/A4/A6`             | worker action, proposal workspace           | [detailed](reference/promote-into-management.md)      |
| `UF-008` | Employee: request medical leave and return         | RequestLeave, ExtendLeave, ReturnFromLeave        | `UF-A3/A4/A6`             | self-service form, evidence, timeline       | [detailed](reference/medical-leave-and-return.md)     |
| `UF-009` | Candidate: accept offer and onboard                | AcceptOffer, StartPreboarding, CompleteOnboarding | `UF-A9/A4`                | candidate portal, tasks, documents          | `FLOW_DRAFT`                                          |
| `UF-010` | Employee/confidential reporter: raise concern      | ReportConcern, OpenInvestigation, AppealCase      | `UF-A5/A4`                | safe report, secure inbox, status           | [detailed](reference/confidential-hr-case.md)         |
| `UF-011` | HR case specialist: resolve service request        | CreateHRRequest, AssignCase, ResolveCase          | `UF-A5/A4`                | Case Center, task/evidence panels           | `FLOW_DRAFT`                                          |
| `UF-012` | HRIS admin: diagnose and repair drift              | CompareSystems, CreateRepairPlan, ExecuteRepair   | `UF-A6`                   | reconciliation and repair workbenches       | [detailed](reference/dataops-reconcile-and-repair.md) |
| `UF-013` | Payroll specialist: resolve run exception          | ResolvePayrollException, CorrectPayroll           | `UF-A6/A4`                | payroll exception workbench                 | `FLOW_DRAFT`                                          |
| `UF-014` | Manager/HR planner: run a population action        | BatchOperation and bounded child intents          | `UF-A7/A2`                | population builder, batch monitor           | `FLOW_DRAFT`                                          |
| `UF-015` | Analyst/manager: ask, inspect and act              | AnalyticalRequest to proposed BusinessIntent      | `UF-A8/A2`                | Ask HCM, result lineage, proposal           | `FLOW_DRAFT`                                          |
| `UF-016` | Operator: intervene in stuck workflow              | Pause/Resume/Retry/Satisfy/Repair workflow        | `UF-A10/A6`               | admin inspector, intervention review        | `FLOW_DRAFT`                                          |
| `UF-017` | Employee: change verified contact endpoint         | ChangePersonalDetails / contact verification      | `UF-A1`                   | self-service form, verification signal      | `FLOW_DRAFT`                                          |
| `UF-018` | Manager: request headcount/requisition             | CreateHeadcountRequest, ApproveHeadcount          | `UF-A2/A4`                | planning form, budget compare               | `FLOW_DRAFT`                                          |
| `UF-019` | Candidate/interviewer: schedule interview          | ScheduleInterview, RecordInterviewFeedback        | `UF-A9/A4`                | appointment picker, feedback task           | `FLOW_DRAFT`                                          |
| `UF-020` | Employee/representative: use assisted route        | any enabled self-service intent                   | composed source archetype | assisted session, paper/phone transcription | `FLOW_DRAFT`                                          |

## Cross-flow experience foundations exposed

The catalog already exposes shared implementation requirements:

```text
ActionDescriptor and action discovery
DraftRevision and safe autosave
FlowDefinition / FlowStageDefinition
server-resolved PageDefinition and AvailableAction
state presentation and multidimensional status
deep-link and resume token lifecycle
notification-to-task-to-intent correlation
simulation/compare/confirmation receipts
form errors, summaries and preserved values
restricted evidence viewer and compartment context
replan/change-summary presentation
reconciliation and repair comparison
accessibility, localization and manual continuity
cross-device/channel parity
privacy-safe product analytics
browser/API/conformance fixture generation
```

These are findings, not permission to create one generic UI framework that owns
domain semantics. Each foundation must resolve to an existing owner or produce
an atomic todo through the flow-gap compiler.

## Next catalog expansion

The catalog expands in this order:

1. Give every accepted BusinessIntent one or more user-flow dispositions:
   participant-facing root, participant-facing child, system-only with visible
   state, admin/operator-only or no user flow.
2. Map each participant-facing intent to an archetype and concrete delta.
3. Expand every mapping through the complete record and MAX-v1.
4. Generate shared and domain-specific findings, todos and exact tests.
5. Add detailed reference documents for materially novel interaction patterns;
   do not generate hundreds of redundant prose files.
