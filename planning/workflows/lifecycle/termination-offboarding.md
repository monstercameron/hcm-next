# Termination and Offboarding

## Identity and scope

```text
workflow_id: lifecycle.termination_offboarding/v1
legacy_intent: employee.termination
target parent intents:
  hcmnext.people.initiate_termination
  hcmnext.people.execute_termination
  hcmnext.lifecycle.start_offboarding
kernel_family: ProcessRequest composed from ChangeRequests and obligations
subject: Person + Worker + Employment + Assignments + WorkforceIdentity
initiator: authorized HR; voluntary-resignation event; governed external signal
state: EXTRACTED + EXPLORED
```

Termination ends an employment relationship at a governed effective time.
Offboarding coordinates downstream consequences. Cancellation after the
employment-ending commit is not process termination; it requires reinstatement or
rehire/correction according to the facts.

## Dependency inventory

Required synchronous:

```text
People/Employment/Assignment
Organization/Position/Relationships
Compensation + final-pay input
Identity/AuthN + AuthZ + separation of duties
Legal/Jurisdiction/Privacy/Records/Holds/Risk
Source Authority + Conflict Registry
Workflow/Human Work/Approval Resolution
Documents/Forms/E-signature where applicable
TransactionPlan + multi-stream commit coordinator + Ledger
```

Required asynchronous or deadline-bound:

```text
Payroll/final pay and tax reporting
Benefits continuation/termination
IAM/application/physical access deprovisioning
Device/equipment recovery
Time/schedule closure
Expense/finance/corporate-card closure
Learning/certification disposition
Messaging/notices
Government/union/works-council notifications
Records retention/legal hold
External observation, reconciliation, repair and incident management
```

## Features

High-risk approval, exact proposal binding, legal obligations, sensitive data,
effective dating, irreversible-boundary analysis, safe-point cancellation,
ordered critical effects, workload priority, documents/notices, timers/deadlines,
multidimensional completion, reconciliation and repair are mandatory. AI may
summarize visible evidence or identify missing inputs; it cannot determine legal
sufficiency, approve, choose termination grounds or execute.

## Steps

```text
1  resolve person/worker/employment, all active assignments and source authority
2  collect termination type/reason, proposed last work date/effective instant,
   notice facts, voluntary evidence and confidentiality compartment
3  resolve jurisdiction, contract/CBA, protected status process requirements,
   legal holds, leave/accommodation/investigation conflicts and decision rights
4  read compensation, accrued time, payroll calendar, benefits, access, devices,
   schedules, expenses, documents and external dependencies
5  validate request completeness without encoding universal legal conclusions
6  compute affected resources/fields/time ranges and detect in-flight conflicts
7  simulate employment end, position vacancy, final-pay inputs, access schedule,
   benefits, notices, equipment, records, cost, deadlines and irreversible effects
8  construct immutable proposal and obligation/effect graph
9  resolve HR/legal/manager/employee-relations/finance approvals conditionally;
   enforce requester != approver and other SoD rules
10 generate required documents/notices and collect signature/acknowledgement only
   where the applicable obligation says so
11 wait for notice period, decision signal or effective time; monitor invalidators
12 immediately revalidate employment, leave/investigation status, authority,
   approvals, law/rule versions, payroll cutoff, conflicts and external readiness
13 CHECKPOINT before irreversible boundary
14 atomically append employment end, assignment ends, position vacancy,
   business ledger/projections and ordered outbox operations
15 dispatch P0 access revocation at the policy-defined instant; dispatch payroll,
   benefits, schedule, finance, device, messaging and filing operations by DAG
16 observe each mandatory target independently; never infer success from submit
17 reconcile final employment, access, payroll, benefits, device and notice state
18 create incidents/RepairPlans for missing access revocation, pay/benefit drift,
   rejected notices, unknown government responses or unreturned equipment
19 preserve records/holds and apply retention schedules
20 complete BusinessState independently from ExternalConsistency,
   ObligationState, ReconciliationState, OperationalState and ClosureState
```

## Cancellation and correction boundaries

```text
before approval              withdraw/cancel proposal
after approval, before commit invalidate approval; cancel or supersede
after notice/signature       legal/process-specific rescission workflow
after employment end commit  reinstate/correct/rehire; never erase history
after external effects       targeted compensation/repair/reprovision actions
```

## Data and candidate properties

```text
TerminationProposal
  person_ref, worker_ref, employment_ref, assignment_refs[]
  termination_type, reason_code, protected_reason_detail_ref?
  notice_date?, last_work_local_date?, effective_instant
  voluntary_evidence_refs[], decision_rights_ref
  jurisdiction_context_ref, contract_cba_refs[]
  position_disposition, manager/org context
  access_revocation_policy_ref, final_pay_context_ref
  benefit_continuation_context_ref, equipment_refs[]
  obligation_set_ref, effect_graph_ref

OffboardingEffect
  effect_type, target_resource_key, criticality, due_at
  predecessor_refs[], reversibility, idempotency_key
  expected_external_version?, authority_fence
  observation_requirement, repair_policy
```

Sensitive employee-relations, medical/accommodation, investigation and legal
evidence remain opaque references unless a step has explicit compartment access.
Derived data includes final-pay estimate, access exposure window, obligation
deadlines, affected application/device lists and completion dimensions.

Legacy gaps to reject:

- A free-text reason plus simple type is insufficient for global execution.
- Generic AI “risk assessment” is not a governance decision.
- Updating `/employment/status` before durable complete planning is unsafe.
- Payroll notification does not cover benefits, IAM, physical access, devices,
  scheduling, documents, records or government obligations.
- External submit success is not final-pay correctness or offboarding completion.
