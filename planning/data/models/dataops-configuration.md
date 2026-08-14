# DataOps, Schema, Reference and Configuration Entities

This file owns bulk ingestion, comparison and controlled configuration change.
Imports never bypass ordinary intents, governance, transactions, evidence, or
reconciliation. Discovered schemas and reference values remain untrusted until
published by an authorized steward.

## Import and staging

```text
ImportJob
  import_id, tenant/org/owner/purpose
  source receipt/artifact, parser/encoding/delimiter metadata
  input/schema/mapping/reference/identity-resolution versions
  authority/governance/classification/residency snapshots
  idempotency/dedup/failure-threshold/concurrency/budget policies
  commit/correction/partial-execution policies, status

ImportRun
  run_id, import job/revision, execution mode
  source artifact/hash, partition/checkpoint refs[]
  started/completed times, counts/control totals/errors/status

ImportBatch
  batch_id, run/range/partition
  lease/fence/priority, input row identity bounds
  staged/item-result/intent/transaction refs[]
  failure threshold/checkpoint/status

StagedRecord
  record_id, run/batch/source row identity
  raw byte/path/cell refs, raw/normalized/canonical digests
  schema/mapping/crosswalk/identity decisions
  proposed intent/transaction refs, quarantine/correction state

StagedCell
  cell_id, record/source path/column/ordinal
  raw bytes/value/presence, parsed/normalized/canonical typed values
  classification/taint, transform/crosswalk/provenance refs
  validation findings/correction/status

DataProfile
  profile_id, source/run, full-scan-or-sample design
  row/field cardinality/null/distribution/schema statistics
  PII classification/quality/freshness/anomaly findings
  profiler/tool/rule versions, evidence/watermark/confidence

ImportValidation
  validation_id, run/record/cell scope
  schema/reference/identity/domain/invariant checks[]
  BLOCKING | WARNING | UNKNOWN | WAIVED findings
  rule versions/reviewer/waiver/evidence/status

ImportSimulation
  simulation_id, run/baseline snapshot digest
  proposal/read/write/effect/conflict/reservation manifests
  counts/blast radius/cost/side-effect profile
  all-or-partial policy, assumptions/unknowns/warnings/digest

ImportCommit
  commit_id, run/simulation/proposal-set digest
  selected records/intent refs, expected baselines/fences
  transaction batch/per-item commit refs
  partial/ambiguous/cancel/correction/reconciliation status

ImportItemResult
  result_id, record/intent/transaction refs
  CREATED | UPDATED | UNCHANGED | SKIPPED | QUARANTINED |
  FAILED | AMBIGUOUS | REPAIR_REQUIRED
  exact diagnostics/effects/observations/correction refs

ImportCorrection
  correction_id, original run/commit/item refs
  causal findings, corrective intent set/simulation
  approval/transaction/effect/reconciliation/verification refs
```

## Batch, export and comparison

```text
BatchOperation
  operation_id, intent/type/owner/purpose
  stable population snapshot/query digest
  chunks/priority/criticality/leases/checkpoints
  rate/budget/failure thresholds
  partial/cancel/compensation/repair policies
  per-item/aggregate/reconciliation status

BatchItem
  item_id, operation/resource/intent refs
  input/proposal/idempotency digests
  lease/fence/attempt/result/effect/repair refs, status

ExportDefinition
  definition_id, version/owner/purpose
  governed query/fields/schema/mapping/format
  current/effective-as-of/known-as-of semantics
  destination/delivery/DLP/retention/schedule policies

ExportJob
  job_id, definition/parameter/source snapshot refs
  authority/authz/DLP/egress decision refs
  population/watermark/count/control totals
  artifact/delivery/receipt/reconciliation refs, status

ExportArtifact
  artifact_id, export job/partition
  schema/format/mapping versions, row count/control totals
  object/digest/encryption/key/classification/expiry refs

ExportDelivery
  delivery_id, job/artifact/destination
  channel/connection/recipient, idempotency/signature
  attempts/acknowledgements/ambiguity/retention/status

SystemComparison
  comparison_id, left/right systems/snapshots
  resource/field/population/effective/known-at scope
  authority/tolerance/normalization policy, item refs[]
  counts/result/repair/status

ComparisonItem
  item_id, comparison/canonical/external resource/field refs
  left/right values and presence = PRESENT | ABSENT | NOT_APPLICABLE |
    REDACTED | STALE | UNAVAILABLE
  authority/freshness/difference/severity/cause/disposition
```

## Schema and data contracts

```text
Schema
  schema_id, canonical name/kind/owner/domain
  global/tenant/org scope, lifecycle

SchemaRelease
  release_id, schema/version/parent
  canonical descriptor/artifact hash
  Protobuf/Go/SchemaFlux generated artifact refs
  compatibility class/policy/effective interval
  publication/signature/deprecation/status

CompatibilityPolicy
  policy_id, schema/kind
  backward/forward/full/none rules
  presence/default/unknown-field/enum/reservation semantics
  version/effective interval

SchemaMigration
  migration_id, source/target releases
  transform/lossiness/reversibility declarations
  fixtures/conformance/results/rollback
  consumer adoption/deadline/status

SchemaConsumerBinding
  binding_id, schema release/consumer
  required range/compatibility, current adoption
  dependency/impact/migration/retirement state

SchemaPublication
  publication_id, release/target environments
  validation/impact/test/approval/signature refs
  rollout/canary/rollback/effective times/status
```

## Reference mastering and crosswalks

```text
ReferenceConcept
  concept_id, dataset/domain/kind/steward
  canonical identity/lifecycle

ReferenceVersion
  version_id, concept/code/names/attributes
  hierarchy/alias/equivalence refs[]
  authority/certification/scope/precedence
  effective interval, DRAFT | ACTIVE | RETIRED | MERGED | SPLIT | SUPERSEDED

ReferenceAlias
  alias_id, concept/version/namespace/value
  locale/source/confidence, effective interval/status

ReferenceHierarchyEdge
  edge_id, dataset/parent/child version refs
  relationship type/ordinal, effective interval
  graph version/authority/cycle validation

CrosswalkResolution
  resolution_id, external system/object/code/as-of
  candidate canonical versions[], confidence/collisions
  UNIQUE | AMBIGUOUS | UNKNOWN | CONFLICTING | RETIRED
  steward decision/evidence/review/expiry

ReferenceValidationResult
  result_id, dataset/release/scope
  uniqueness/collision/hierarchy/consumer checks
  blockers/warnings/unknowns/steward/trace/status

ReferenceMigrationPlan
  plan_id, source/target concepts or releases
  aliases/crosswalks/consumer dependencies
  data/config/workflow migration actions
  blockers/approvals/rollback/reconciliation/status
```

## Configuration packaging and promotion

```text
ConfigurationPackage
  package_id, name/version/owner/source environment
  content-addressed manifest of workflow/policy/schema/mapping/reference/
  rule/agent/template/feature/connectors refs
  dependency lock/compatibility target scopes
  artifact/digest/signature/provenance/status

ConfigurationDependency
  dependency_id, package/component/consumer refs
  required version/range, hard-or-soft, reason
  resolution/migration/blocker status

ConfigurationImpactResult
  impact_id, package/target environment
  affected tenant/org/workflow/intent/data/integration/population refs
  compatibility/security/legal/cost/operational findings
  tests/simulations/unknowns/blockers/digest

ConfigurationPublication
  publication_id, package/target environment/scope
  impact/conformance/approval/signature refs
  canary/ramp/cutover/fence/effective times
  activation observations/reconciliation/rollback/status

ConfigurationRollback
  rollback_id, publication/prior known-good package
  trigger/reason/authority/approval
  compatibility/data migration/effect/reconciliation plan
  executed/verified times/status

JobDefinition
  job_definition_id, type/version/owner
  input/output schemas, capability/workflow binding
  schedule/trigger/priority/criticality/resource/retry policies
  concurrency/idempotency/retention/status

JobRun
  run_id, definition/version/trigger
  parameter/source snapshot, lease/fence/checkpoints
  output artifact/effect/metrics/error refs
  started/completed/status/repair
```

## Required invariants

```text
External IDs require an effective-dated correlation decision.
Missing inbound rows never imply deletion without an explicit absence policy.
Discovery never publishes schemas, mappings, or reference values.
Every import item compiles to governed BusinessIntent execution.
Ambiguous external commits are observed before retry.
Original bytes, receipts, operations, and prior config remain immutable.
Retirement is blocked while hard consumers lack a migration.
Exports always pass current AuthZ, purpose, DLP, destination, and egress checks.
Configuration publication is content-addressed, signed, reversible where stated,
and reconciled after activation.
```
