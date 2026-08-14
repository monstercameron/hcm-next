# Business Rules and Decision Entities

Regulatory `RulePack` entities remain specialized legal content. These entities
own customer policy, eligibility, formulas, thresholds and decision tables that
are deterministic but not inherently legal rules.

```text
BusinessRuleDefinition
  rule_id, canonical name/domain/owner/purpose
  input/output schemas, applicability scope
  rule family/composition strategy, lifecycle

BusinessRuleVersion
  version_id, rule/parent version
  expression/decision-table/formula refs
  canonical source/compiled digests
  effective interval, authority/publication/signature/status

RuleExpression
  expression_id, language/version, typed AST
  referenced fields/functions/constants/units
  determinism/purity/side-effect proof, digest

DecisionTable
  table_id, rule version, hit policy
  typed input/output columns, ordered row refs
  default/no-match/ambiguous-match behavior
  effective interval/digest

DecisionTableRow
  row_id, table/priority
  typed predicates/outputs, explanation code
  effective interval/status

RuleInputSnapshot
  snapshot_id, evaluation/subject/context
  typed input values with source authority/provenance
  effective/known-at/source watermarks
  missing/unknown/redacted/stale values, canonical digest

BusinessRuleEvaluation
  evaluation_id, rule version/input snapshot
  output/result = PASS | FAIL | PARTIAL | UNKNOWN | NOT_APPLICABLE
  matched rows/expression steps/intermediate values
  warnings/obligations/restrictions, evaluated-at/valid-until
  deterministic trace/output digest

RuleConflict
  conflict_id, subject/rule-family/evaluation refs
  incompatible outputs/strategies/precedence
  severity/unknowns/resolution policy/status

RuleOverride
  override_id, evaluation/conflict/subject
  requested output/scope/reason/evidence
  authority/approval/SoD/effective interval/expiry
  prohibited legal override proof, status

RulePublication
  publication_id, rule version/target scopes
  schema/compatibility/dependency/impact refs
  fixtures/conformance/approval/signature
  canary/shadow/activation/rollback/status

RuleTestFixture
  fixture_id, rule version/name
  typed inputs/expected outputs/expected trace
  boundary/negative/unknown cases, source/version

PolicyImpactAnalysis
  analysis_id, proposed rule version
  affected population/dependency snapshots
  changed decisions/obligations/costs/risks/unknowns
  simulation/approval/remediation refs, digest
```

Invariants:

```text
Rule evaluation is deterministic for identical canonical input and version.
UNKNOWN never becomes PASS or ALLOW by default.
Published versions are immutable; corrections publish successors.
Customer overrides cannot bypass mandatory law or tenant isolation.
Every rule output exposes the exact input snapshot and explanation trace.
```
