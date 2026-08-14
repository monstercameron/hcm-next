# Data Quality and Invariant Evaluation Contract

The Quality Service owns quality assessments, findings, exceptions, and
remediation campaigns. Each Domain owns its hard business invariants and exposes
them through the common evaluation protocol. The Quality Service cannot waive a
domain invariant.

## Canonical State

```text
QualityRule
InvariantRule
RuleSet
EvaluationPlan
EvaluationRun
Finding
Exception
Override
RemediationCampaign
Verification
```

```text
Rule: DRAFT -> VALIDATED -> APPROVED -> ACTIVE -> SUPERSEDED | QUARANTINED
Run:  PLANNED -> RUNNING -> COMPLETE | PARTIAL | TIMED_OUT | FAILED
Finding: OPEN -> ACKNOWLEDGED -> REMEDIATING -> VERIFIED -> CLOSED
         |-> EXCEPTED(until) | FALSE_POSITIVE
```

Rules declare owner/domain, type, schema/fields, scope, effective time, required
source watermarks/freshness, evaluation points, synchronous/asynchronous mode,
cost/time/memory/population limits, severity, blocking/quarantine behavior,
exception eligibility, evidence, and version.

## APIs and Evaluation Semantics

```text
quality.rules.propose|validate|publish|quarantine
quality.evaluate|profile|status|explain
invariants.validate|verify
findings.query|acknowledge|disposition
exceptions.request|approve|expire
overrides.request|approve|revoke
remediation.plan|execute|verify
```

Result is `PASS`, `FAIL`, `UNKNOWN`, or `NOT_APPLICABLE`; `UNKNOWN` is never
coerced to `PASS`. Missing/stale source watermarks, timeout, partial scan, rule
error, or unavailable dependency yield `UNKNOWN/PARTIAL` and follow the declared
blocking/quarantine policy. Required synchronous invariants must complete inside
their resource budget or block the command; expensive customer rules are moved to
bounded asynchronous assessment and cannot consume unbounded hot-path resources.

Evaluation points include ingest/stage, simulation, proposal submission,
commit-time domain validation, post-commit projection, reconciliation, scheduled
sampling, and repair verification. Domain invariants run again at commit even if
preflight passed.

## Security, Override, and Evidence

Rule author, domain owner, security/privacy reviewer where data access changes,
publisher, exception approver, override executor, and remediation verifier are
separate according to severity. Customer rules are tenant-scoped, compiled by
SchemaFlux to a bounded pure plan, and cannot perform network/file/domain writes.
An invariant override is a high-risk typed business operation with reason,
scope, expiry, compensating obligations, step-up/dual approval, and after-action
review; some invariants are explicitly non-overridable.

Evidence records rule/set/snapshot, source watermarks and completeness, inputs or
protected references, execution build, resource/time use, result/explanation,
finding, exception/override and expiry, remediation effects, verification, false-
positive feedback, and retained trend. Signed local rule snapshots govern hot
path evaluation.

Gate A implements pilot schema validity, completeness, reference validity,
compensation/position plausibility, graph cycle and source freshness checks. Gate
B adds commit-time domain invariants, projection/outbox/reconciliation checks,
bounded override tests, and repair verification.
