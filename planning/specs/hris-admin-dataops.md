# HRIS Admin Toolkit and DataOps

This specification defines an adjacent product surface built from the same governed capabilities needed to operate HCM Next. It does not expand HCM Next into payroll, recruiting, benefits, or another HCM domain suite.

## Product Thesis

HRIS administrators spend substantial time importing, mapping, comparing, debugging, correcting, promoting, and explaining data across systems. HCM Next already needs those primitives for ChangeOps.

> When HCM Next builds an internal operation to diff, replay, inspect, map, validate, simulate, reconcile, explain, or promote, evaluate whether the same operation can safely become a governed customer capability.

```text
                 HCM NEXT PLATFORM KERNEL

 schema + authority + validation + temporal state + transactions
               + ledger + integrations + repair
                              |
                              v
                    HRIS ADMIN TOOLKIT
                              |
       +----------+-----------+-----------+-----------+
       v          v           v           v           v
     Import     Compare     Diagnose     Repair     Configure
       |          |           |           |           |
       +----------+-----------+-----------+-----------+
                              v
                        HRIS DATAOPS
```

The toolkit follows three rules:

1. It invokes semantic capabilities rather than reaching around the platform into databases or queues.
2. Preview, simulation, approval, execution, reconciliation, and repair use the same contracts as ordinary ChangeOps.
3. An internal tool becomes a customer product only when its authorization, tenant isolation, evidence, usability, support ownership, and marginal operating cost are acceptable.

## The P1A Slice and Its Product Test

P1A ships four operator surfaces because the Promotion pilot cannot run
without them:

```text
Cross-system diff             the intended-versus-observed view
Effective-date debugger       "what was effective" vs "what did we know"
Connector test bench          read-only; redrive is P1B
AuthZ explainer               why this field is masked for this principal
```

The product hypothesis is that these four are worth paying for on their own,
independent of any write path. The test is stated now so it cannot be
rationalized later:

```text
DataOps is a product candidate when, during the P1A pilot:
  - a partner administrator opens the diff or debugger without being asked
    to, at least weekly, for four consecutive weeks, and
  - at least one incumbent-system defect is found through the diff before the
    partner's own process finds it, and
  - the partner names a price they would pay for the four surfaces alone.

Otherwise DataOps stays an operator toolkit and its remaining candidates are
not scheduled.
```

Everything below the P1A slice is a candidate list, deliberately unranked.

## Capability Families

```text
HRIS DataOps

├── Import
│   ├── stage
│   ├── profile
│   ├── map
│   ├── validate
│   ├── simulate
│   └── commit
│
├── Export
│   ├── query
│   ├── snapshot
│   ├── schedule
│   └── deliver
│
├── Compare
│   ├── systems
│   ├── effective periods
│   ├── organizations
│   └── configurations
│
├── Diagnose
│   ├── provenance
│   ├── effective dates
│   ├── identity
│   ├── data quality
│   ├── idempotency
│   ├── workflow execution
│   ├── communication delivery
│   └── authorization
│
├── Repair
│   ├── redrive
│   ├── rebuild
│   ├── reconcile
│   └── bulk correct
│
└── Configure
    ├── mappings
    ├── reference data
    ├── schemas
    ├── policies
    ├── packages
    └── promotion
```

## Candidate Inventory (unranked)

Value is assigned by partner use, not by this table. Candidates are listed by
the foundation they reuse so that a later decision can see what each one
costs.

| Candidate                                 | Reused foundation                                      |
| ----------------------------------------- | ------------------------------------------------------ |
| Data Import and Staging                   | Schemas, mappings, validation, transactions, reconcile |
| Cross-System Data Diff (P1A)              | Observations, authority, reconciliation                |
| Effective-Date Debugger (P1A)             | Bitemporal ledger and projections                      |
| Bulk Change Planner                       | TransactionPlan, conflict, simulation, workflow        |
| AuthZ Explainer (P1A)                     | Explainable authorization decisions                    |
| Connector Test Bench (P1A)                | Connector manifests, mappings, secrets, fakes          |
| Failed Integration Redrive (P1B)          | Outbox, idempotency, observation, RepairPlan           |
| Reference-Data Crosswalk                  | Master/reference data and external mappings            |
| Organization Graph Validator              | Organization graph, data quality, invariants           |
| Data Quality Rules                        | Validation, quality, invariant execution               |
| Identity and Duplicate Finder             | Identity claims, matching, search, review              |
| Configuration Diff and Promotion          | Versioned configuration, bundles, rollout, rollback    |
| Ledger and Provenance Query               | Assertion classes, causality, provenance graph         |
| Schema, Capability, Dependency Inspectors | Registries and manifests                               |
| Webhook and Event Simulator               | Event schemas, subscriptions, signing, replay          |
| Mapping Transformation Engine             | Source/canonical/destination transforms                |
| Scheduled Extract and Snapshot Export     | Governed query, temporal queries, delivery             |
| Impact Analysis                           | Provenance/dependency graph and manifests              |
| Idempotency Inspector                     | Command/event identity and suppression evidence        |
| Field-Level Audit Export                  | Ledger, field lineage, authorization, evidence         |
| Workflow Execution Inspector              | Compiled plan, durable nodes, tasks, attempts, traces  |
| Communication Delivery Inspector          | Intent, audience, render, attempts, evidence, signals  |

These are logical tools, not services. A candidate is scheduled only after the
P1A product test passes and a partner names it.

## First Six Operator Surfaces

### 1. Data Import and Staging

```text
CSV / XLSX / API / bounded SFTP
              |
              v
       immutable staging artifact
              |
      profile + detect schema
              |
              v
 source -> canonical field mapping
              |
 identity + reference-data resolution
              |
              v
 validation + quality + invariant checks
              |
       +------+------+
       |             |
     errors       valid rows
       |             |
 correction UI       v
               transaction simulation
                      |
                   approval
                      |
               resumable commit
                      |
                reconciliation
```

An import never becomes a hidden bulk database write. Valid rows compile into ordinary `BusinessIntent` or `HCMChangeRequest` instances, grouped under one population-scoped `ChangeRequest` parent.

```text
ImportBatch

batch_id
source_artifact_ref
source_schema
mapping_version
identity_resolution_version
reference_snapshot
row_count
validation_summary
transaction_ids[]
checkpoint
status
```

Large batches declare write sets, tenant budgets, concurrency, checkpoint size, failure threshold, rollback/compensation policy, and reconciliation target. Rows remain independently idempotent so retry resumes rather than repeats completed changes.

### 2. Cross-System Data Diff

```text
                       worker Jane
                            |
       +--------------------+--------------------+
       v                    v                    v
 HCM Next projection   Workday observation   Payroll observation
       |                    |                    |
       +--------------------+--------------------+
                            v
             authority-aware normalized comparison
                            |
           +----------------+----------------+
           |                |                |
         match          tolerated drift    mismatch
                                             |
                                      diagnosis graph
                                             |
                                         RepairPlan
```

The engine distinguishes absent/not-applicable/redacted/stale/unavailable values from actual inequality. It compares canonical semantic values after versioned transformations and shows which source was authoritative for each field and effective interval.

### 3. Effective-Date Debugger

```text
field: compensation.base

effective timeline
Jan 1       Jun 1             Sep 1
$120k ------ $130k ----------- $145k future
                 ^
                 |
      correction recorded Aug 14
      Jun 1 should be $135k

known-at timeline
Jun 15: system believed $130k
Aug 15: system knew corrected $135k
```

The debugger supports:

```text
history.explain(
  subject,
  field,
  effective_at?,
  known_at?,
  source_authority?,
  include_claims?
)
```

Results label domain facts, external observations, claims, corrections, workflow proposals, and projection versions. “What was effective?” and “what did HCM Next know?” remain separate questions.

### 4. AuthZ Simulator and Explainer

```text
principal + delegated/agent context
              x
capability + resource + fields + purpose + time
              |
              v
      side-effect-free policy evaluation
              |
       +------+------+----------------+
       v             v                v
 decision       visible fields    obligations
       |             |                |
       +------+------+----------------+
              v
  matched grants + inherited denies + policy versions
```

Simulation cannot mint authority or bypass current enforcement. It uses an explicit policy snapshot for historical/configuration testing and clearly labels whether the result is `CURRENT`, `AS_OF_POLICY_VERSION`, or hypothetical.

### 5. Connector Operations: Test Bench and Redrive

This is the customer/operator surface over the runtime contracts defined by the [Integration Platform](integration-platform.md). DataOps does not create a second connector execution path.

```text
Connector Test Bench                 Failed Operation
       |                                   |
 validate manifest                        v
 resolve secret reference           inspect request/response
 test network/auth                  classify failure
 run synthetic fixture                    |
 preview mapping                     +----+----+
 mock provider response              |         |
 validate transformation           retry    repair mapping/config
       |                              |         |
       +--------------+---------------+---------+
                      v
             idempotent redrive of effect
                      |
               observe + reconcile
```

Redrive re-executes a specific failed external effect, not the whole business transaction. It preserves the original transaction and operation identity, creates a new attempt, uses the current approved credential, and records whether configuration changed since the original attempt. Material mapping changes may require a new RepairPlan approval.

Synthetic tests cannot silently target production endpoints or real workers. Environment, destination, data classification, and egress policy are verified before delivery.

### 6. Configuration Diff and Promotion

```text
Sandbox Bundle v18                 Production Bundle v17
        |                                  |
        +------------ semantic diff -------+
                           |
              workflows | mappings | policy
              reference data | schemas | agents
                           |
                    dependency/impact scan
                           |
      12,491 workers | 418 live workflows | 2 connectors
                           |
               validate + conformance tests
                           |
                       simulation
                           |
                   approval + signature
                           |
                  staged/canary promotion
                           |
                  verify or roll back pointer
```

A deployment package is immutable and content-addressed:

```text
ConfigurationBundle

bundle_id
manifest_version
component_refs[]
dependency_lock
target_scope
source_environment
compatibility_result
impact_analysis_ref
test_evidence_refs[]
approval_refs[]
signature
```

Rollback normally activates the prior compatible bundle; it does not erase publication history or reverse already-executed business transactions.

## Remaining Tools by Shared Primitive

### Bulk Planning and Correction

`BulkChangePlan` wraps many independently idempotent transaction plans:

```text
population/query or staged rows
          -> expand affected resources
          -> validate all
          -> detect inter-row and external conflicts
          -> reserve constrained resources where required
          -> simulate totals and blast radius
          -> approve exact batch revision
          -> execute by priority/checkpoint
          -> pause on threshold
          -> reconcile every row
```

Bulk authorization is separate from single-record authority. The plan enforces maximum population, fields, cost, error rate, concurrency, export restrictions, and dual control.

### Reference Crosswalk and Mapping Transformation

```text
source value             canonical reference          destination value
Workday ENG4  ---------> job://software/L4 ---------> Payroll 88142
                             |
                    effective-dated mapping
                    version + scope + confidence
```

Transformations are pure and versioned where possible. They declare input/output schemas, null semantics, enum mappings, locale/currency/time behavior, lossiness, validation, test fixtures, and reversibility. Arbitrary customer code is not the initial model.

### Quality, Organization, and Identity Diagnostics

```text
incoming/current facts
       |
       +-- validation: type/rule conformance
       +-- quality: completeness/plausibility/freshness
       +-- invariant: graph/system correctness
       +-- identity: duplicate/merge candidates
       |
       v
issue -> evidence -> owner -> correction/merge review -> verify
```

Organization validation includes cycles, orphan nodes, overlapping exclusive parents, invalid manager relationships, position overfill, inactive managers, impossible effective intervals, and broken legal-entity references.

Duplicate detection produces candidates and evidence, never automatic merge for uncertain matches. Merge and separation remain governed identity transactions.

### Inspectors and Explorers

Schema, capability, provenance, dependency, and idempotency inspectors share a read-only administrative query surface:

```text
Admin question
      |
      v
governed registry/provenance query
      |
      +-- schema versions and compatibility
      +-- capability input/output/risk/AuthZ
      +-- dependency and affected-consumer graph
      +-- field source/transform/decision history
      +-- duplicate suppression and retry evidence
```

Inspectors enforce the same tenant, organization, field, purpose, and sensitive-domain controls as operational APIs. “Administrative” never means unrestricted.

### Extract, Snapshot, Webhook, and Audit Delivery

```text
governed query or event fixture
              |
       scope + fields + purpose
              |
       point-in-time semantics
        CURRENT / EFFECTIVE_AS_OF / KNOWN_AS_OF
              |
        size + cohort + DLP checks
              |
      render CSV/JSON/event/evidence package
              |
          Egress Gateway
              |
     download / SFTP / webhook / API
              |
      delivery and retention evidence
```

Scheduled extracts pin query, schema, mapping, destination, credential reference, delivery calendar, and policy version. They re-evaluate current authorization, legal, egress, and destination eligibility before every run.

Event simulation uses synthetic or explicitly approved sanitized payloads by default. Production event replay is a separate high-risk capability with idempotency, destination, and side-effect safeguards.

## Capability API Surface

```text
dataops.imports.stage
dataops.imports.profile
dataops.imports.validate
dataops.imports.simulate
dataops.imports.commit

dataops.compare.systems
dataops.history.explain
dataops.bulk.plan
dataops.bulk.execute

dataops.authz.simulate
dataops.provenance.explain
dataops.idempotency.explain
dataops.dependencies.find

dataops.connectors.test
dataops.connectors.operations.inspect
dataops.connectors.operations.redrive
dataops.connectors.reconcile

dataops.reference.crosswalk.read
dataops.reference.crosswalk.publish
dataops.mappings.preview
dataops.mappings.publish

dataops.quality.evaluate
dataops.organizations.validate
dataops.identities.find_duplicates

dataops.config.diff
dataops.config.impact
dataops.config.packages.create
dataops.config.promote

dataops.schemas.inspect
dataops.capabilities.explore
dataops.events.simulate

dataops.extracts.define
dataops.extracts.run
dataops.snapshots.export
dataops.audit.export
```

Each manifest declares read/write domains, organization scope, purpose, bulk limits, data classification, simulation support, side effects, approval requirements, idempotency, cost, egress, and evidence.

## Security and Safety Boundary

The toolkit can amplify access and mutations, so it requires stronger controls than ordinary single-record screens:

- Bulk, export, redrive, configuration publish, reference mapping, and identity merge use distinct capabilities.
- Preview does not imply execute authority.
- AuthZ simulation does not disclose fields the simulator cannot inspect.
- Connector secrets remain references and are never returned to the UI or agent.
- Production payload inspection is classified, purpose-bound, redacted, and audited.
- Exports and webhook tests pass the Data Egress Gateway.
- Redrive preserves idempotency and cannot change the original business ledger event.
- Configuration and mapping publication requires immutable revision, impact analysis, approval, signature, and rollback plan.
- Agent access begins read/analyze/draft only; bulk execution and production redrive require deterministic policy and human authority.

## Go-Only Implementation Shape

```text
internal/dataops/
  staging/          profiling/        compare/
  history/          bulkplan/         authzsim/
  connectorops/     crosswalk/        mapping/
  quality/          orgvalidate/      identitymatch/
  configdiff/       promotion/        provenance/
  schemainspect/    capabilityview/   extract/

api/proto/dataops/v1/
  import.proto      compare.proto     history.proto
  connector.proto   config.proto      inspect.proto
  export.proto

ui/admin/dataops/
  GoWebComponents workspaces
```

Phase 1 should not introduce a separate DataOps service. The modules live with the modular Go platform and use the same repositories, capability interceptors, transaction engine, outbox, and work-item system. Separate workers are justified for large imports, extracts, comparisons, and redrives because those require independent resource governance.

Use low-cost/open tooling at boundaries:

- Go standard `encoding/csv` and streaming readers for CSV
- A maintained Go XLSX reader only when a design partner requires XLSX
- PostgreSQL staging tables, `COPY`, and advisory/transaction locks where appropriate
- Protobuf descriptors and the schema registry for inspection
- S3-compatible objects for staged inputs and generated exports
- OpenTelemetry for operational traces without including sensitive row contents

## Phasing

| Capability group                             | P1A                                        | P1B                                  | Productization gate                           |
| -------------------------------------------- | ------------------------------------------ | ------------------------------------ | --------------------------------------------- |
| Cross-system diff and reconciliation view    | **IMPLEMENT** for pilot fields             | same                                 | The P1A product test above                    |
| Effective-date and provenance debugger       | **IMPLEMENT** read-only                    | same                                 | The P1A product test above                    |
| AuthZ explainer                              | **IMPLEMENT** for pilot policies           | same                                 | The P1A product test above                    |
| Connector test bench                         | **IMPLEMENT** read-only, first connector   | add redrive                          | Redrive evidence and idempotency tests        |
| Import/staging                               | Operator CSV load for pilot seed data only | same                                 | Partner names it after the product test       |
| Configuration diff and promotion             | **OUT**                                    | Only for shipped versioned artifacts | Multiple environments need repeatable changes |
| Crosswalk and mapping                        | First connector mappings only              | same                                 | Second system proves reusable semantics       |
| Bulk change, scheduled extract, snapshot     | **OUT**                                    | **OUT**                              | Partner names it; workload/DLP limits proven  |
| Identity finder, graph validator, quality UI | **OUT**                                    | Internal checks only                 | Partner names it                              |
| General explorers, packages, event simulator | **OUT**                                    | **OUT**                              | Registry `MANAGED` profile and demand         |

## Success Measures

- Time to stage, validate, correct, and reconcile a customer import
- Percentage of data mismatches with an explainable authority/provenance path
- Mean time to diagnose effective-date and integration problems
- Failed external operations safely redriven without repeating business transactions
- Configuration changes promoted without manual drift
- Support cases resolved by customer administrators without privileged platform access
- Bulk or export operations stopped before exceeding authorization, DLP, cost, or error thresholds
- Percentage of internal operator capabilities safely reused as customer capabilities
- Toolkit adoption independent of the initiating Promotion workflow

## Product Boundary

HRIS DataOps is an adjacent operator product, not a reason to widen the authoritative domain model prematurely. It makes incumbent HR stacks easier to operate while reinforcing the ChangeOps wedge:

```text
observe and diagnose
        -> safely configure and import
        -> govern changes
        -> reconcile and repair
        -> earn broader transaction authority
```

The decisive advantage is not that each individual utility is novel. It is that every utility shares one authority model, temporal model, capability fabric, transaction contract, ledger, and repair system.
