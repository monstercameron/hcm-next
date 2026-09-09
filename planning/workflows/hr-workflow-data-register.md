# HR Workflow Data and Property Register

This is an exploratory union of data discovered across the first twelve modeled
workflows. It is not a database schema. A property graduates only after authority,
classification, temporal semantics, validation and Protobuf ownership are agreed.

## Shared identity and context

```text
TenantContext
  tenant_id, organization_scope_refs[], placement_epoch, residency_policy_ref

PrincipalContext
  principal_id, principal_type, session_assurance, delegation_ref?, purpose

SubjectSet
  person_ref?, worker_ref?, employment_refs[], assignment_refs[]
  position_refs[], organization_refs[], relationship_refs[]

WorkflowExecutionContext
  intent_instance_ref, workflow_definition/version
  execution_mode, correlation_id, risk_class, criticality
  locale_context_ref, jurisdiction_context_ref
  source_authority_snapshot_ref, governance_snapshot_ref
```

## Person and relationship properties

```text
Person
  person_id, identity_resolution_ref, status, preferred_locale

StructuredPersonName
  given_names[], middle_names[], family_names[], prefixes[], suffixes[]
  local_full_name, latin_full_name?, script, ordering_rule

ContactPointRevision
  contact_point_id, type, value_normalized, verification_state
  purpose_tags[], effective_range, source_authority, revision

AddressRevision
  address_id, address_type, lines[], locality, administrative_area
  postal_code, country_code, normalized_address_ref?, validation_status
  effective_range, source_authority, revision

EmergencyContactRelationship
  relationship_id, subject_person_ref, contact_person_ref?
  supplied_name, relationship_code, endpoints[], priority
  effective_range, verification_state, source_authority
```

## Employment, assignment and organization properties

```text
Worker
  worker_id, person_ref, worker_number?, worker_type, lifecycle_status

Employment
  employment_id, worker_ref, legal_entity_ref, employment_type
  start/end facts, status, jurisdiction_context_ref, contract_refs[]

AssignmentRevision
  assignment_id, employment_ref, assignment_type, primary
  job_ref, position_ref?, organization_ref, location_ref
  cost_center_ref?, FTE_decimal, effective_range, source_authority

ManagerRelationshipRevision
  relationship_id, worker_assignment_ref, manager_person_ref
  relationship_type, effective_range, source_authority, graph_version

Position
  position_id, job_ref, org/location/legal_entity refs
  capacity_decimal, capacity_unit, occupancy policy, lifecycle, effective_range
```

## Compensation and budget properties

```text
CompensationPackageRevision
  package_id, employment/assignment refs, component_revision_refs[]
  effective_range, source_authority

CompensationComponentRevision
  component_id, component_type, operation
  amount_decimal?, rate_decimal?, percent_decimal?, currency?
  frequency, basis, units?, proration/annualization/rounding rule refs
  taxability_ref?, effective_range, correction/supersession refs

Reservation
  reservation_id, resource_ref, quantity_decimal, unit/currency?
  proposal_digest, fence, expires_at, status, consumption_ref?

HeadcountRequest
  request_id, plan/budget/org/job/location/cost-center refs
  position_count, capacity_decimal, target_date, priority, justification
```

## Lifecycle and protected-case properties

```text
TerminationProposal
  employment/assignment refs, type, reason_code, protected_detail_ref?
  notice/last-work/effective times, evidence refs, obligation/effect graph refs

LeaveCase
  case_id, employment_ref, protected_compartment_ref
  requested/approved/used ranges, intermittent_pattern?
  program_entitlement_refs[], evidence_refs[], deadlines[], status

Candidate/Application/Offer
  candidate/person-link refs, requisition/application refs
  consent/notice refs, assessment decision refs
  offer component refs, conditions, document/signature refs
```

## Workflow control and evidence properties

```text
ProposalRevision
  proposal_id, revision, canonical_digest, input/artifact hashes
  reads, write_set, conflicts, obligations, effect_graph
  policy/schema/reference/rule versions, estimated cost/risk

ApprovalRequirement / ApprovalDecision / ApprovalBinding
  resolver, candidates, cardinality/quorum, SoD, deadline
  decision, reason, approver/delegation/session evidence
  exact proposal digest, authority snapshots, invalidators

TransactionPlan
  expected stream heads, planned appends/projections/outbox
  reservations/fences, source-authority decisions, commit digest

ExternalOperation
  operation_id, connector/connection, semantic operation
  external_resource_key, causal sequence/predecessor
  expected external version, authority fence, payload digest
  idempotency key, attempts, ambiguous-result state

Observation
  observer/source authority, resource/value, source version/watermark
  observed_at, recorded_at, confidence/completeness, payload digest

ReconciliationResult
  expected_ref, observed_ref, dimensions[], PASS/FAIL/UNKNOWN/PARTIAL
  differences[], watermark, repair_required

RepairPlan
  diagnosis, affected resources, corrective actions, reversibility
  approval/SoD, simulation, execution and verification evidence
```

## Cross-workflow feature matrix

Legend: `R` required, `C` conditional, `-` not applicable to the modeled scope.

| Workflow                    | Eff. date | Approval | Evidence/docs | Reservation | Multi-stream | External | Regulatory | Reconcile/repair |
| --------------------------- | --------: | -------: | ------------: | ----------: | -----------: | -------: | ---------: | ---------------: |
| Contact information         |         R |        C |             C |           - |            C |        C |          C |  R when external |
| Emergency contact           |         R |        C |             C |           - |            C |        C |          - |  R when external |
| Legal name                  |         R |        R |             R |           - |            R |        R |          R |                R |
| Manager change              |         R |        C |             - |           - |            R |        C |          - |                R |
| Compensation change         |         R |        R |             C |           C |            R |        R |          C |                R |
| Promotion to management     |         R |        R |             C |           R |            R |        R |          C |                R |
| Org transfer + compensation |         R |        R |             C |           R |            R |        R |          R |                R |
| Headcount requisition       |         R |        R |             - |           R |            R |        C |          C |                R |
| Recruit/hire/onboard        |         R |        R |             R |           R |            R |        R |          R |                R |
| Leave/return                |         R |        R |             R |           C |            R |        R |          R |                R |
| Termination/offboarding     |         R |        R |             R |           - |            R |        R |          R |                R |
| Payroll correction/retro    |         R |        R |             R |           - |            R |        R |          R |                R |

## Dependency matrix

| Dependency                          | Workflows that require it                                                                      |
| ----------------------------------- | ---------------------------------------------------------------------------------------------- |
| People/Identity Resolution          | all subject-centered workflows                                                                 |
| Organization/Relationships          | manager, promotion, transfer, headcount, hire, termination                                     |
| Position/Headcount                  | promotion, transfer, headcount, hire, termination                                              |
| Compensation/Budget                 | compensation, promotion, transfer, headcount, hire, termination, payroll correction            |
| Payroll                             | compensation, promotion, transfer, hire, leave, termination, payroll correction                |
| Benefits                            | hire, leave, termination, payroll correction where deductions change                           |
| IAM/Access                          | manager, promotion, transfer, hire, leave conditionally, termination                           |
| Documents/Forms/Human Work          | legal name, all approval workflows, hire, leave, termination, payroll correction               |
| Communications                      | verification, tasks, notices, offers, leave, termination and repair                            |
| Regulatory/Jurisdiction             | legal name, compensation conditionally, transfer, hire, leave, termination, payroll correction |
| Integration/Observation             | every workflow with external authority/effects                                                 |
| Commit/Ledger/Reconciliation/Repair | every mutating workflow                                                                        |

## Unresolved modeling decisions

1. Whether ContactPoint and Address are Person-owned or worker/employment-scoped
   for every country and customer.
2. Which manager relationship variants affect AuthZ, approvals and directory
   reporting, and their precedence.
3. Whether compensation package changes are one intent or parent/child intents.
4. The boundary between headcount authorization, position creation and recruiting
   requisition creation.
5. Which onboarding readiness dimensions block a start versus permit degraded
   manual continuity.
6. How leave program concurrency/offset algorithms and protected evidence are
   represented across jurisdictions.
7. Termination access-revocation timing relative to notice, last work time and
   employment end.
8. Payroll-correction authority when Human Capital Management Suite is observer/controller rather than
   payroll system of record.
