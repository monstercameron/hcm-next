# Workflow Engine Step-Type Catalog

This exploratory catalog expands the normative primitive list in
`planning/specs/workflow-runtime.md`. It does not add new primitive classes.

## Common node envelope

Every step definition and execution uses a common envelope:

```text
NodeDefinition
  node_id
  node_type
  input_mapping
  input_schema_ref
  output_schema_ref
  required_context[] ContextRequirement
  timeout_policy_ref?
  retry_policy_ref?
  failure_route
  cancellation_behavior
  side_effect_profile
  idempotency_policy_ref?
  safe_point_behavior
  evidence_policy_ref
  metadata

NodeExecution
  execution_id, workflow_instance_id, node_id, attempt
  tenant_id, cell_id, placement_epoch, organization_scope
  node_definition_digest, input/output schema digests
  context_snapshot_refs[] + governance_snapshot_refs[]
  authority_ref, purpose, classification, data_access_manifest_ref
  causal_predecessor_refs[] + correlation_id
  effective_interval_ref + evaluated_known_at + policy_as_of
  status, lease/fence, input_digest, output_ref/digest
  started_at, completed_at, retry_at?, error_ref?
  capability/decision/task/agent/artifact/external refs as applicable
  trace_id, recorded_at

ContextRequirement
  context_kind + field_paths[]
  required_schema_ref/digest
  purpose + maximum_classification
  maximum_age + required_watermarks[]
  invalidator_kinds[]
  missing/stale/redacted behavior
  projection_policy_ref
```

Universal compiler rules:

- Input/output mappings resolve against immutable typed schemas.
- Required context is minimum necessary; no node receives the entire worker.
- Every execution proves tenant/cell/placement, context/governance versions,
  authority, purpose/classification and causal lineage. A stale placement epoch,
  missing invalidator check or context outside `ContextRequirement` fails before
  node logic runs.
- Material executions bind the business effective interval, `known_at` evaluation
  point and policy-as-of instant explicitly; runtime timestamps never substitute
  for business or legal time.
- Retry is allowed only when the operation is pure/read-only or has compatible
  idempotency and ambiguity recovery.
- Every outcome has one explicit route; there is no implicit “first edge.”
- `UNKNOWN`, timeout, cancellation and exhausted retry behavior are declared.
- Material outputs are referenced from the ledger; transport diagnostics remain
  telemetry.

## 1. CAPABILITY

Invokes one versioned governed capability owned by a domain or platform service.

```text
specific fields:
  capability_ref, operation_mode, authority_scope
  idempotency_key_mapping?, expected_versions?, effect_binding?
outputs:
  typed capability result + governance/execution/effect references
```

The Capability Gateway resolves entitlement, AuthZ, legal/privacy/risk, purpose,
source authority and rate/admission decisions. The workflow engine does not
implement the capability's business semantics. Mutations require stable effect
identity and return committed/ambiguous/rejected outcomes. Compiler checks schema
compatibility, declared read/write/effect sets, simulation support and retry
safety. Evidence includes request/output digests, capability version, governance
snapshot, idempotency result and transaction/effect references.

## 2. DECISION

Selects exactly one typed route from already available deterministic input.

```text
specific fields:
  decision_table_ref? | expression_ref
  routes[] {route_key, predicate/result}
  default_route?                 // explicit, never positional
outputs:
  route_key, evaluated_input_digest, evaluation_trace_ref
```

`DECISION` does not call an agent, query mutable data or mutate state. Mutable
facts must be read by an earlier capability and supplied as a snapshot. Compiler
checks exhaustiveness, mutually exclusive routes or declared precedence, and an
explicit `UNKNOWN` route when inputs may be incomplete.

## 3. APPROVAL

Creates or waits on proposal-bound `ApprovalRequirement` records and returns the
aggregate result.

```text
specific fields:
  requirement_refs[] | requirement_factory_ref
  proposal_digest_mapping
  resolver_time_policy
  candidate_churn_policy
  expiry/escalation/delegation/SoD policies
outputs:
  APPROVED | REJECTED | INVALIDATED | EXPIRED | CANCELLED
  approval_binding_refs[]
```

Recipient resolution is separate from decision authority. Authority and exact
proposal binding are rechecked at decision and execution time. The node is durable
and normally does not retry user decisions; duplicate submissions replay the same
decision identity. Evidence includes requirements, resolved candidates, votes,
authority/session/delegation, reasons, quorum and invalidators.

## 4. TASK

Creates governed human work that returns a typed submission other than approval.

```text
specific fields:
  task_type, assignee_expression, queue_ref?
  form_definition_ref, required_output_schema_ref
  claim/delegation/escalation/SLA/visibility policies
outputs:
  typed submission, completion/verification refs
```

Examples include collecting missing data, resolving an identity match, reviewing
a discrepancy or uploading evidence. Claims use leases/CAS; reassignment cannot
duplicate completion. Compiler checks that every required output is consumed or
retained intentionally and that accessible/non-digital alternatives exist where
required.

## 5. WAIT

Suspends durably until a time or calendar condition.

```text
specific fields:
  wake_condition                    // instant, local date, business offset, deadline
  timezone/calendar/tzdb refs
  reference_update_policy           // PIN, RECOMPUTE, REQUIRE_REVIEW, MIGRATE, BLOCK
  early/late/cancellation routes
outputs:
  timer firing ref, resolved instant, lateness/reference-impact evidence
```

The scheduler owns the timer; no executor sleeps. Wakeup is at-least-once and
deduplicated by timer identity. Clock uncertainty or changed calendar references
follow explicit routes. A timer firing never proves the business action remains
valid; later revalidation is required.

## 6. SIGNAL

Suspends durably until a correlated event or message is accepted.

```text
specific fields:
  event_type/schema_ref, correlation_filter
  external_integration_receipt_ref?  // mandatory for external-origin signals
  source/trust eligibility, dedupe/ordering policy
  timeout and late-signal policy
outputs:
  accepted signal ref + normalized typed payload + taint manifest
```

Signals may originate from webhooks, messaging replies, document signatures,
background checks or domain events. The node validates schema, tenant, source,
signature, correlation and authorization before acceptance. Duplicate and late
signals are recorded but do not repeat continuation. Payloads remain tainted until
transformed/validated by the required capability or rule.

## 7. PARALLEL

Schedules a bounded set of independent branches.

```text
specific fields:
  branch_entries[]
  max_concurrency, priority/cost budget
  read_consistency_vector_ref, consistency_mode
  cancellation/failure propagation policy
outputs:
  branch execution refs; no implicit aggregate business result
```

The compiler rejects conflicting declared writes/effects unless a serialization
or merge proof exists. Dynamic fan-out requires a bounded population snapshot and
per-item idempotency. `PARALLEL` starts branches; only `JOIN` decides how their
results combine.

## 8. JOIN

Waits for and combines declared parallel or child results.

```text
specific fields:
  branch_refs[]
  mode = ALL | ANY | QUORUM | REQUIRED_SET | BEST_EFFORT
  required_count?, required_branch_classes[]
  timeout/failure/cancellation policy
outputs:
  aggregate result + per-branch status/output refs
```

`JOIN` never turns missing or failed mandatory branches into success. Best-effort
completion preserves degraded/unknown dimensions. Compiler checks that every
branch is accounted for and that `ANY`/quorum cancellation does not abandon an
unsafe side effect.

## 9. SUBWORKFLOW

Starts a pinned child workflow and binds its outcome to the parent.

```text
specific fields:
  child_workflow_ref/version
  input_mapping, child_idempotency_key_mapping
  child_authority_scope, expansion_certificate_ref?
  wait_mode = WAIT | DETACH_WITH_OBLIGATION
  cancellation/migration/compensation propagation
outputs:
  child instance/ref, typed child result, taint manifest and completion dimensions
```

The parent cannot silently inherit a later child version. Recursive cycles and
depth/fan-out are bounded at compile time. Parent cancellation records each child
as cancelled, compensated, already complete or unable to cancel. Detached child
work leaves an explicit obligation and correlation link.

The effective child scope is the intersection of parent-approved, caller and
child-policy scopes. Expansion requires a separately approved certificate and
is verified again at parent closure.

## 10. TRANSFORM

Applies a versioned deterministic, side-effect-free data mapping.

```text
specific fields:
  transform_ref/version
  input/output schema refs
  input_taint_manifests[], taint_join_policy, sanitizer_receipt_ref?
  normalization profile, lookup snapshot refs?
outputs:
  typed transformed value + trace/digest + taint manifest
```

Use for canonicalization, field mapping and constructing typed inputs—not for
policy decisions or domain validation. External/reference lookups must be pinned
inputs. Compiler or conformance tests require deterministic golden vectors,
resource bounds and no arbitrary customer code.

## 11. RULE

Evaluates a versioned deterministic business/policy rule or decision table.

```text
specific fields:
  rule_ref/version, rule_family
  composition_strategy?
outputs:
  PASS | FAIL | UNKNOWN | PARTIAL, result values, explanation trace
```

Rules may calculate requirements or validate typed facts but do not mutate domain
state. Missing/stale inputs route explicitly. Legal rules retain jurisdiction,
source and interpretation versions; an agent cannot replace a rule result.
When `rule_family` is legal/regulatory, the output schema must carry the canonical
`LegalEvaluationStatus`, `LegalEffect[]`, `ObligationBinding[]`, composition trace
and coverage status. A generic `PASS` cannot discard restrictions or obligations.

## 12. AGENT

Invokes a governed agent capability for bounded interpretation or analysis.

```text
specific fields:
  agent_ref/version, task_profile_ref
  allowed_tool_capabilities[], data/trust budget
  required_output_schema_ref, output_validator_ref
  input_taint_manifest_ref, required_output_taint_schema_ref
  sanitizer_profile_ref?, taint_downgrade_policy
  model/provider eligibility and cost/deadline policy
outputs:
  typed proposal/analysis + citations/provenance/uncertainty + taint manifest
```

Agent output is derived/untrusted until schema, grounding, policy and business
validation pass. The agent receives no direct provider credentials or ambient
workflow data. Mutating actions require a later deterministic capability and all
normal governance. Prompt/tool content follows telemetry privacy and retention
rules. Kill switch, timeout, budget exhaustion and hostile-input routes are
mandatory.

## 13. DOCUMENT

Generates, collects, validates, delivers, acknowledges or signs a typed artifact.

```text
specific fields:
  operation, template/document_type ref
  locale/jurisdiction, recipient/signer expression
  assurance_mode                    // NATIVE_EVIDENCE, EXTERNAL_PROVIDER, MANUAL_GATE
  identity_proof/signature capability refs when required
  delivery/evidence requirement, classification/retention
outputs:
  artifact ref/hash, validation/delivery/signature/evidence-package refs
```

Document bytes live in the artifact plane. Provider acceptance is not delivery,
acknowledgement or legal signature. Malware/DLP, signer identity, ceremony,
template/render versions, expiration and declined/timed-out routes are explicit.
`NATIVE_EVIDENCE` is unavailable until native evidence and identity-proofing
capabilities have implementation/conformance evidence. Until then, signature
operations must select `EXTERNAL_PROVIDER` or `MANUAL_GATE`, retain the provider
or manual evidence reference, and may not claim native assurance.

## 14. OBSERVE

Reads external or derived state and optionally reconciles it against expected
state.

```text
specific fields:
  observer_capability_ref, expected_state_mapping?
  authority/source/watermark requirements
  comparison_profile_ref?, freshness/deadline policy
outputs:
  Observation + optional ReconciliationResult
```

The result distinguishes `PASS`, `FAIL`, `UNKNOWN` and `PARTIAL`. Submission
receipts cannot substitute for observation. Reads are idempotent but bounded
retry ends in an explicit unknown/degraded state and may create repair/incident
work.

## 15. CHECKPOINT

Establishes a durable operational safe point before or after an atomic/irreversible
region.

```text
specific fields:
  checkpoint_kind, consistency_requirements[]
  pause/cancel/migration eligibility
outputs:
  checkpoint ref, workflow version/state digest, outstanding-effect summary
```

A checkpoint performs no business mutation. It verifies durable state and records
where pause, migration, cancellation or recovery may safely act. Compiler rejects
checkpoints that falsely split an atomic commit or leave an ambiguous external
effect unaccounted for.

## 16. COMPENSATE

Invokes a declared corrective capability for a completed or ambiguous prior
effect.

```text
specific fields:
  target_execution/effect ref
  compensation_capability_ref
  reason/authority/approval policy
  verification_observation_ref
outputs:
  CompensationResult + verification/remaining-drift refs
```

Compensation is not rollback or history deletion. It is independently authorized,
idempotent, evidenced and verified. Irreversible effects route to manual/repair
handling rather than fictitious compensation. Failure produces `REPAIR_REQUIRED`
or an incident; it never restores success automatically.

## 17. END

Produces a typed terminal runtime result without equating runtime completion to
business success.

```text
specific fields:
  terminal_code
  completion_mapping for business/external/reconciliation/obligation/
    operational/outcome/closure dimensions
  outstanding obligation/repair/incident refs
outputs:
  immutable WorkflowResult
```

Every reachable terminal path ends explicitly. Compiler checks required output
fields, unresolved mandatory branches/effects and closure eligibility. Examples
include completed-consistent, completed-degraded, rejected, cancelled,
superseded, quarantined and repair-required.

## Legacy-node mapping

Legacy node types are extraction evidence only:

| Legacy type                       | Target primitive/composition                                                        |
| --------------------------------- | ----------------------------------------------------------------------------------- |
| `interaction`                     | `TASK`, `APPROVAL`, `DOCUMENT`, or explicit external UI around a capability         |
| `block`                           | normally `CAPABILITY`; `TRANSFORM`/`RULE` only when manifest proves those semantics |
| `policy_check`                    | governance inside `CAPABILITY`, or typed `RULE` + `DECISION`                        |
| `approval` / `approval_gate`      | `APPROVAL`                                                                          |
| `transaction_plan`                | `CAPABILITY transaction.plan`                                                       |
| `data_write` / `projection_write` | domain `CAPABILITY`; workflow never writes storage directly                         |
| `external_write`                  | Integration `CAPABILITY` followed by `OBSERVE`                                      |
| `ledger_event`                    | evidence emitted by responsible service; not a standalone target step               |
| `manual_repair`                   | `TASK` or repair `SUBWORKFLOW`; correction uses `COMPENSATE`/capabilities           |
| `ai_review`                       | governed `AGENT` followed by typed validation and deterministic decision boundaries |
| `terminal`                        | `END`                                                                               |
