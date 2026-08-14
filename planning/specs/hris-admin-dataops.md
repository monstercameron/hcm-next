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

## System Inventory

| #   | Capability                        | Reused foundation                                      | Product value |
| --- | --------------------------------- | ------------------------------------------------------ | ------------- |
| 1   | Data Import and Staging           | Schemas, mappings, validation, transactions, reconcile | Very high     |
| 2   | Cross-System Data Diff            | Observations, authority, reconciliation                | Very high     |
| 3   | Effective-Date Debugger           | Bitemporal ledger and projections                      | Very high     |
| 4   | Bulk Change Planner               | TransactionPlan, conflict, simulation, workflow        | Very high     |
| 5   | AuthZ Simulator and Explainer     | Explainable authorization decisions                    | Very high     |
| 6   | Connector Test Bench              | Connector manifests, mappings, secrets, fakes          | High          |
| 7   | Failed Integration Redrive        | Outbox, idempotency, observation, RepairPlan           | Very high     |
| 8   | Reference-Data Crosswalk          | Master/reference data and external mappings            | Very high     |
| 9   | Organization Graph Validator      | Organization graph, data quality, invariants           | High          |
| 10  | Data Quality Rules                | Validation, quality, invariant execution               | Very high     |
| 11  | Identity and Duplicate Finder     | Identity claims, matching, search, review              | High          |
| 12  | Configuration Diff                | Versioned configuration and provenance                 | Very high     |
| 13  | Configuration Promotion           | Bundles, sandbox, approval, rollout, rollback          | Very high     |
| 14  | Ledger and Provenance Query       | Assertion classes, causality, provenance graph         | Very high     |
| 15  | Schema and Contract Inspector     | Protobuf/schema registry and compatibility             | High          |
| 16  | API and Capability Explorer       | Capability Registry and manifests                      | High          |
| 17  | Webhook and Event Simulator       | Event schemas, subscriptions, signing, replay          | High          |
| 18  | Mapping Transformation Engine     | Source/canonical/destination transforms                | Very high     |
| 19  | Scheduled Extract                 | Governed query, artifacts, delivery                    | Very high     |
| 20  | Point-in-Time Snapshot Export     | Effective/recorded time and temporal queries           | High          |
| 21  | Impact Analysis                   | Provenance/dependency graph and manifests              | Very high     |
| 22  | Dependency Finder                 | Registry relationships and version adoption            | High          |
| 23  | Change Set and Deployment Package | Configuration bundle and promotion                     | High          |
| 24  | Idempotency Inspector             | Command/event identity and suppression evidence        | Medium        |
| 25  | Field-Level Audit Export          | Ledger, field lineage, authorization, evidence         | High          |
| 26  | Workflow Execution Inspector      | Compiled plan, durable nodes, tasks, attempts, traces  | Very high     |
| 27  | Communication Delivery Inspector  | Intent, audience, render, attempts, evidence, signals  | High          |

These are logical tools, not 27 services. Phase 1 should implement shared Go packages and a small number of admin capability families.

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

An import never becomes a hidden bulk database write. Valid rows compile into ordinary `BusinessIntent` or `HCMChangeRequest` instances, grouped under a `BatchOperation`.

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

| Capability group                             | Phase 1 posture                                         | Productization gate                                      |
| -------------------------------------------- | ------------------------------------------------------- | -------------------------------------------------------- |
| Import/staging for pilot configuration/data  | Implement bounded CSV/API path                          | Repeated admin use and safe resumable commit             |
| Cross-system diff and reconciliation view    | Implement for pilot fields and systems                  | Useful independently of the Promotion workflow           |
| Effective-date and provenance debugger       | Implement read-only operator view                       | Design partners can self-diagnose without support access |
| AuthZ simulator                              | Implement for pilot policies                            | Explanation is safe, complete, and supportable           |
| Connector test bench and redrive             | Implement for the first connector                       | Redrive evidence and idempotency pass failure tests      |
| Configuration diff and promotion             | Implement only for shipped versioned artifacts          | Multiple environments/customers need repeatable changes  |
| Crosswalk and mapping                        | Minimal contract plus first connector mappings          | Second system proves reusable mapping semantics          |
| Bulk change, scheduled extract, snapshot     | Design/conformance only unless required by a paid pilot | Workload, DLP, and resumability limits are proven        |
| Identity finder, graph validator, quality UI | Reuse internal checks; expose selectively               | False-positive and remediation experience is acceptable  |
| General explorers, packages, event simulator | Later implementation                                    | Registry maturity and customer demand                    |

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
