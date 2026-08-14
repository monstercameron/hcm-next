# Exploratory Workflow Modeling Contract

Every workflow exploration document MUST make the following responsibilities
explicit. `UNKNOWN` is a valid discovery result; omission is not.

## Workflow identity

```text
workflow_id
business_intent_type
kernel_family
owner_domain
subject_types[]
initiator_expressions[]
discovery_state
source_evidence[]
```

## Features

Feature flags describe behavior required from the platform, not UI toggles:

```text
self_service
effective_dating
simulation
approval
human_task
evidence_collection
document_generation
signature_or_acknowledgement
reservation
multi_stream_commit
external_effects
messaging
regulatory_evaluation
sensitive_compartment
reconciliation
repair
bulk
offline_draft
```

Every enabled feature names the capability and evidence it needs. Every disabled
feature states `NOT_APPLICABLE`, `DEFERRED`, or `PROHIBITED`.

## Dependency classes

```text
KERNEL
  BusinessIntent, proposal/digest, workflow runtime, human work,
  TransactionPlan/commit coordinator, ledger, timers/signals

GOVERNANCE
  Identity/AuthN, AuthZ, source authority, legal, privacy/DLP,
  risk, entitlement, conflict/reservation, data quality/invariants

DOMAIN
  People, Organization, Position, Compensation, Payroll, Benefits,
  Time, Talent, Learning, Access, Documents, Communications

CONNECTIVITY
  connector definitions/connections, mappings/crosswalks, operation journal,
  external ordering, observations, webhook/file/message adapters

DATA
  canonical records, reference/master data, projections, artifacts,
  provenance, records/retention, search/analytics where applicable

OPERATIONS
  admission/priority, idempotency, retries, incident route, telemetry,
  reconciliation, repair, recovery, support access
```

Dependencies are classified as `REQUIRED_SYNC`, `REQUIRED_ASYNC`,
`CONDITIONAL`, `MANUAL_FALLBACK`, or `OUT_OF_SCOPE`.

## Step record

Each step has:

```text
step_id
primitive
purpose
actor_or_executor
reads[]
writes[]
capabilities[]
governance_checks[]
input_schema_ref
output_schema_ref
side_effect_profile
idempotency_scope
timeout_retry_policy
safe_point
failure_route
evidence_events[]
```

The allowed primitive vocabulary comes from the workflow runtime:

```text
CAPABILITY DECISION APPROVAL TASK WAIT SIGNAL PARALLEL JOIN SUBWORKFLOW
TRANSFORM RULE AGENT DOCUMENT OBSERVE CHECKPOINT COMPENSATE END
```

Legacy node types are mapped into this vocabulary; they do not expand the target
kernel.

## Data inventory

Every workflow classifies its data:

| Data class         | Required fields                                                                              |
| ------------------ | -------------------------------------------------------------------------------------------- |
| Subject references | tenant, person/worker/employment/assignment/position IDs as applicable                       |
| Current facts      | field owner, value/version, effective interval, recorded-at/source authority                 |
| Proposed facts     | typed value, reason, effective interval, proposer, proposal revision/digest                  |
| Context            | tenant, org, locale, jurisdiction, purpose, risk, execution mode                             |
| Decisions          | requirement, candidate resolver, authority snapshot, result/reason, proposal digest          |
| Artifacts          | artifact reference/hash, classification, malware status, retention/hold                      |
| Effects            | resource key, operation, payload digest, ordering sequence, authority fence, idempotency key |
| Observations       | external authority, source watermark/version, observed value/time, confidence                |
| Outcome            | business, external-consistency, reconciliation, obligation, operational and closure states   |

Money is fixed-decimal with currency and rounding profile. Time uses the shared
half-open effective range and distinguishes local dates from instants. IDs are
tenant-scoped opaque references; a globally unique-looking ID never substitutes
for tenant binding.

## Minimum workflow phases

```text
1 DISCOVER       resolve subject, authority, dependencies and applicable version
2 COLLECT        collect typed request, evidence and purpose
3 PREFLIGHT      validate schema, quality, invariants, AuthZ/legal/privacy/risk
4 SIMULATE       produce reads, writes, obligations, cost, conflict and effect plan
5 DECIDE         collect exact-digest-bound approvals or human work
6 SCHEDULE       reserve resources and wait for effective time/signals
7 REVALIDATE     recheck all mutable assumptions immediately before commit/send
8 COMMIT         atomically append owned domain facts, ledger, projection and outbox
9 EFFECT         dispatch external/human/message/file effects in declared order
10 OBSERVE       obtain authoritative downstream observations
11 RECONCILE     compare expected and observed state
12 CLOSE/REPAIR  close only eligible dimensions or create a bounded RepairPlan
```

## Property-discovery output

Each workflow ends with candidate properties under four headings:

```text
Input properties
Canonical/domain properties
Runtime/evidence properties
Derived/read-model properties
```

Properties marked `LEGACY` are extracted behavior. `TARGET` properties are design
candidates. `UNRESOLVED` properties need a domain or legal decision.
