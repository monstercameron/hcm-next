# Business Intent Catalog and Runtime Model

## Decision

The 530-name catalog supplied for HCM Next is accepted as the initial semantic
vocabulary backlog. It does not create 530 implementations, workflow node types,
database tables, or public RPC methods.

```text
human | system | agent | schedule | rule | external event
                            |
                            v
                  semantic IntentDefinition
                            |
                 versioned typed input/output
                            |
                            v
                     IntentInstance
                            |
              governance + control resolution
                            |
                            v
          one stable kernel family and execution plan
                            |
       +----------+---------+---------+----------+
       v          v         v         v          v
    workflow   domain    filing     case      analysis
               command   gateway    work       query
                            |
                            v
                  evidence and outcome
```

The seven kernel families are:

| Family                | Meaning                                                          | May create business mutation?                    |
| --------------------- | ---------------------------------------------------------------- | ------------------------------------------------ |
| `CHANGE_REQUEST`      | Proposes one governed domain-state change                        | Yes, after approval/revalidation when required   |
| `PROCESS_REQUEST`     | Requests a durable multi-step business process                   | Through child intents/domain capabilities        |
| `CALCULATION_REQUEST` | Requests deterministic computation                               | No, unless a separate change consumes the result |
| `FILING_REQUEST`      | Produces/submits a regulated filing package                      | Yes; submission can be irreversible              |
| `CASE`                | Owns a long-lived service, investigation, or confidential matter | Through explicitly linked child intents          |
| `BATCH_OPERATION`     | Applies one bounded operation to a resolved population           | Through per-item child intents/transactions      |
| `ANALYTICAL_REQUEST`  | Requests governed read, explanation, comparison, or inference    | No                                               |

`Case` is deliberately included. Omitting it would force investigations and HR
service matters into mutation or workflow abstractions that do not own case
participants, confidentiality, evidence, disposition, and reopening.

## Catalog identity

Display names are not identifiers. Every definition uses:

```text
intent_type_id = hcmnext.<domain>.<verb_noun>
version        = monotonically increasing positive integer
definition_ref = intent_type_id + "/v" + version
```

Examples:

```text
hcmnext.people.create_person/v1
hcmnext.people.promote_worker/v1
hcmnext.payroll.run_payroll/v1
hcmnext.regulatory.calculate_tax/v1
hcmnext.cases.open_investigation/v1
hcmnext.analytics.analyze_turnover/v1
```

Domain qualification resolves catalog collisions. In particular, the two
`GrantEntitlement` names are different definitions:

```text
hcmnext.access.grant_entitlement/v1
hcmnext.commercial.grant_entitlement/v1
```

Renaming a display label does not change identity. Changing material input,
output, authorization, side-effect, cancellation, evidence, or outcome semantics
requires a new definition version. Reusing a retired identifier for different
semantics is prohibited.

## Required definition fields

Each of the 530 proposed candidates must pass from `CATALOGUED` through
`DRAFT_CONTRACT` to `CONTRACTED` before it
can be published and invoked. No field below is inferred from its name:

```text
IntentDefinition
  catalog_number
  intent_type_id
  version
  display_name
  description
  owner_plane
  owner_domain
  kernel_family
  maturity
  input_schema_ref
  result_schema_ref
  allowed_initiators[]
  allowed_execution_modes[]
  side_effect_profile
  risk_class
  data_classification_floor
  subject_kinds[]
  required_capabilities[]
  governance_requirements[]
  preconditions[]
  invariants[]
  idempotency_scope
  conflict_footprint_rule
  proposal_binding_rule
  revalidation_rule
  cancellation_rule
  compensation_rule
  evidence_rule
  retention_class
  outcome_contract
  SLO_class
  availability_policy
  phase_depth
```

Maturity is explicit:

```text
CATALOGUED  name, number, description and proposed owner exist
DRAFT_CONTRACT metadata is being specified but referenced contracts do not all resolve
CONTRACTED  all required semantics and schemas are reviewable
COMPILED    SchemaFlux validation and generated registry succeed
PUBLISHED   immutable version is available in an environment
DEPRECATED  no new callers; existing compatibility policy applies
RETIRED     invocation prohibited; historical resolution preserved
```

The supplied mega-catalog is an accepted 530-candidate input, but it is not yet a
complete repository source catalog. Fourteen definitions are currently recorded
as `DRAFT_CONTRACT`; the other 516 names/descriptions still require lossless
ingestion into partition files before they may honestly be called `CATALOGUED`.
Only the Phase 1 promotion slice may advance to `CONTRACTED` or beyond under the
present delivery plan.

The values 530, 14 and 516 are planning-intake counts, not independently
reproducible catalog evidence, until the original numbered candidate manifest is
checked in with provenance and digest. CI completeness claims are prohibited until
that source exists and partitions are generated or reconciled against it.

## Instance envelope

An instance references a definition and never relies on a free-form name:

```text
IntentInstance
  intent_id                 UUIDv7
  definition_ref
  tenant_id
  organization_scope_id
  billing_account_id?
  initiator
  delegation_chain[]
  purpose
  subjects[]
  requested_effective_at?
  request_payload           typed Protobuf + schema reference
  idempotency_key
  correlation_id
  causation_id?
  trace_id
  classification
  retention_class
  control_snapshot_refs
  canonical_request_digest
  lifecycle_dimensions
  created_at
  execution_mode
  instance_version
  recorded_at
  last_transition_at
  origin_event_ref?
  source_authority_snapshot_digest
  risk_context_digest
  proposal_revisions[]
  approval_bindings[]
  execution_bindings[]
  cancellation_decisions[]
  supersession_references[]
  correction_references[]
  closure_records[]
  lifecycle_compatibility_profile_ref
```

The typed payload contains Protobuf wire bytes whose descriptor matches the
definition's `input_schema_ref`. Ordinary Protobuf serialization is not the
approval/idempotency canonical form: the versioned canonicalization profile
projects material fields and produces the bound `CanonicalDigestReference`.
Arbitrary JSON, `map[string]any`, and a model prompt are not valid authoritative
request payloads.

## Independent lifecycle dimensions

The catalog model retains the kernel's independent dimensions:

```text
IntentState
ProposalState
ApprovalState
ExecutionState
ExternalConsistency
ReconciliationState
ClosureState
BusinessState
OperationalState
ObligationState
OutcomeState
```

It adds no universal `status`. A payroll process can have runtime completion with
external consistency still pending; an analytical request can complete with no
proposal, approval, execution mutation, or reconciliation state.

## Composition rules

One parent intent may create child intents, but the relationship is explicit:

```text
RunPayroll (PROCESS_REQUEST)
   +-- CalculatePayroll (CALCULATION_REQUEST)
   +-- ApprovePayroll (CHANGE_REQUEST: approval evidence)
   +-- ReleasePayroll (CHANGE_REQUEST)
   +-- ReconcilePayroll (PROCESS_REQUEST)
```

```text
SendBulkCommunication (BATCH_OPERATION)
   +-- immutable AudienceSnapshot
   +-- one SendMessage child per delivery subject/batch partition
   +-- aggregate result that never hides per-recipient failure
```

Parent completion cannot overwrite child truth. Child idempotency, authorization,
classification, evidence, and repair remain independently addressable.

## Intent, capability, workflow, and transaction boundaries

```text
Intent       what an authorized actor wants accomplished or answered
Capability   a governed semantic operation available to accomplish part of it
Workflow     durable coordination of capabilities, humans, time, and signals
Transaction  atomic authoritative mutation and evidence commit
Outcome      later evidence about whether the intent achieved its purpose
```

An intent may resolve directly to a pure capability without starting a workflow.
For example, `ExplainWorkerState` is analytical. `PromoteWorker` normally compiles
to a workflow and eventually one multi-stream domain transaction. `OpenInvestigation`
creates a Case and related human work. `SubmitGovernmentReport` invokes a filing
gateway only after an immutable filing package and approvals exist.

## Initiator and exposure policy

Allowed initiators are explicit per definition:

```text
HUMAN | AGENT | SERVICE | INTEGRATION | SCHEDULE | RULE | SYSTEM_EVENT
```

System/trigger-driven entries 513-530 are not automatically public APIs. Their
definitions default to service, schedule, rule, or system-event initiation and
require a separate capability manifest before any human or partner invocation.
Agent initiation never increases authority: entitlement, AuthZ, legal, privacy,
risk, budget, DLP, and tool-gateway decisions still apply.

## Initial draft-contract slice

Phase 1 starts with these definitions and does not claim the rest are executable:

| Catalog # | Definition                                       | Family                | Side effect         | Phase depth                       |
| --------: | ------------------------------------------------ | --------------------- | ------------------- | --------------------------------- |
|        28 | `hcmnext.people.change_manager/v1`               | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | Design/conformance only           |
|        36 | `hcmnext.people.explain_worker_state/v1`         | `ANALYTICAL_REQUEST`  | `READ_ONLY`         | Gate A implement                  |
|        53 | `hcmnext.people.promote_worker/v1`               | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | Gate A contract; Gate B implement |
|        69 | `hcmnext.rewards.change_base_pay/v1`             | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | Gate A contract; Gate B implement |
|        81 | `hcmnext.rewards.simulate_compensation/v1`       | `CALCULATION_REQUEST` | `PURE`              | Gate A implement                  |
|        82 | `hcmnext.rewards.evaluate_pay_band_position/v1`  | `CALCULATION_REQUEST` | `PURE`              | Gate A implement                  |
|        84 | `hcmnext.rewards.reserve_compensation_budget/v1` | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | Gate B implement                  |
|        85 | `hcmnext.rewards.release_compensation_budget/v1` | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | Gate B implement                  |
|       366 | `hcmnext.work.approve_proposal/v1`               | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | Gate B implement                  |
|       367 | `hcmnext.work.reject_proposal/v1`                | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | Gate B implement                  |
|       457 | `hcmnext.intelligence.explain_transaction/v1`    | `ANALYTICAL_REQUEST`  | `READ_ONLY`         | Gate A implement                  |
|       461 | `hcmnext.operations.detect_drift/v1`             | `ANALYTICAL_REQUEST`  | `READ_ONLY`         | Gate A implement                  |
|       463 | `hcmnext.operations.create_repair_plan/v1`       | `PROCESS_REQUEST`     | `READ_ONLY`         | Gate A implement                  |
|       464 | `hcmnext.operations.simulate_repair/v1`          | `CALCULATION_REQUEST` | `PURE`              | Gate A implement                  |

Promotion is a composite `ChangeRequest`; it does not silently alias the base-pay
child request. The proposal binds the exact child set, versions, material inputs,
expected stream sequences, reservations, required approvals, and execution order.

## Catalog coverage partitions

The accepted catalog is partitioned explicitly for ownership and future source
files. Catalog number is stable and never reused.

| Numbers | Source partition                     | Owner                         |
| ------: | ------------------------------------ | ----------------------------- |
|    1-39 | People / Core HR                     | People domain                 |
|   40-67 | Jobs, Positions & Organization       | Workforce domain              |
|   68-88 | Compensation & Rewards               | Rewards domain                |
|  89-110 | Payroll                              | Payroll domain                |
| 111-130 | Tax & Regulatory                     | Regulatory plane              |
| 131-142 | Benefits                             | Rewards / Benefits domain     |
| 143-160 | Time, Attendance & Scheduling        | Workforce domain              |
| 161-176 | Leave & Absence                      | Workforce / Leave domain      |
| 177-199 | Recruiting & ATS                     | Talent / Recruiting domain    |
| 200-217 | Onboarding & Offboarding             | People lifecycle domain       |
| 218-235 | IAM / Workforce Access               | Access domain                 |
| 236-249 | Talent & Performance                 | Talent domain                 |
| 250-260 | Learning & Skills                    | Talent / Learning domain      |
| 261-274 | Employee Experience & Communications | Messaging + Experience        |
| 275-286 | HR Service Delivery & Cases          | Case Management               |
| 287-297 | Employee Relations & Investigations  | Case Management / ER          |
| 298-308 | Global Mobility & Immigration        | People + Regulatory           |
| 309-316 | Safety & Workplace                   | Workforce / Safety            |
| 317-333 | Privacy & Data Governance            | Governance / Privacy          |
| 334-346 | Documents, Forms & Signatures        | Connectivity / Documents      |
| 347-368 | Workflow & Human Work                | Workflow / Human Work         |
| 369-384 | Integration & External Systems       | Integration plane             |
| 385-404 | DataOps & Administration             | DataOps / Control             |
| 405-418 | Authorization, Security & Identity   | Identity / Governance         |
| 419-437 | Reporting & Analytics                | Intelligence plane            |
| 438-460 | Semantic & Agent Intelligence        | Agent + Intelligence          |
| 461-481 | Reconciliation, Repair & Operations  | Operations / Assurance        |
| 482-497 | Billing & Commercial                 | Commercial plane              |
| 498-512 | Tenant / Platform Administration     | Control plane                 |
| 513-530 | System / Trigger-Driven Intents      | Trigger owner + target domain |

Every partition file must declare all entries in its numeric range. CI must fail
for missing/duplicate numbers, duplicate `(intent_type_id, version)`, unqualified
IDs, unknown schemas/capabilities, invalid family/side-effect combinations, or a
published definition with `CATALOGUED` or `DRAFT_CONTRACT` maturity.

## Go-only implementation boundary

Canonical request/result contracts are Protobuf. SchemaFlux consumes the
structured intent definitions to validate relationships and emit Go registries,
documentation, compatibility reports, fixtures, and dependency indexes. GWC
discovers permitted definitions through Go capability clients. grpcbridge adapts
external transports to the same gRPC services. No TypeScript catalog, UI model,
Node generator, or parallel JSON schema is permitted.

The initial canonical Protobuf contract is
[`schema/proto/hcmnext/intents/v1/business_intent.proto`](../../schema/proto/hcmnext/intents/v1/business_intent.proto).
Generated Go is not checked in until the repository pins the Protobuf toolchain;
handwritten duplicate message types are prohibited.

## Acceptance tests

1. Catalog numbers are unique, complete within each source partition, and never reused.
2. Domain-qualified IDs distinguish access and commercial entitlement grants.
3. Every published version resolves input/output Protobuf descriptors and capabilities.
4. Deterministic serialization produces the same canonical request digest in every Go process.
5. `ANALYTICAL_REQUEST` and `CALCULATION_REQUEST` definitions cannot declare mutation side effects.
6. `FILING_REQUEST` with submission side effects declares irreversibility and correction semantics.
7. `BATCH_OPERATION` preserves per-item identities, results, failures, and retry safety.
8. An agent cannot invoke a definition absent a published capability and governance approval.
9. A renamed display label preserves definition identity; material contract change increments version.
10. Historical instances remain resolvable after definition deprecation or retirement.

## Status and ownership

- Owner: Business Transaction Service and Capability Registry owners
- Status: kernel contract defined; 530-candidate vocabulary accepted; 14 draft definitions recorded; 516 candidates awaiting source ingestion
- Phase 1: only the explicit contracted slice above
- Long term: contract partitions incrementally as a funded domain requires them
