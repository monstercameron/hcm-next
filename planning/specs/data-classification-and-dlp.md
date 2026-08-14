# Data Classification, Label Propagation, and DLP Contract

The Classification and DLP Service owns the taxonomy, classification policies,
runtime decisions, derived-label propagation, and controlled declassification.
Domain/schema owners propose labels; ordinary application code cannot lower them.

## Canonical State

```text
ClassificationTaxonomy
ClassificationPolicy
DataLabel
DerivedLabel
DeclassificationDecision
DLPIntent
DLPDecision
PropagationReceipt
```

```text
Policy: DRAFT -> VALIDATED -> APPROVED -> ACTIVE -> SUPERSEDED | QUARANTINED
Label:  PROPOSED -> RESOLVED -> ACTIVE -> RECLASSIFICATION_REQUIRED -> SUPERSEDED
Declassification: REQUESTED -> TRANSFORMED -> REVIEWED -> APPROVED -> EXPIRED/REVOKED
```

Labels cover schema fields, rows/resources, free text, documents/artifacts,
messages, event payloads, derived projections, search/semantic fragments,
analytics, telemetry, agent inputs/outputs/memory, exports, and external payloads.
Each label carries taxonomy/version, class/categories, tenant/org/subject scope,
source/provenance, purpose limits, residency/recipient constraints, retention,
effective interval, confidence for content-derived classification, and consumers.

## Resolution, Propagation, and APIs

Static schema labels combine with domain/runtime/content-derived labels. Default
combination is `MOST_RESTRICTIVE`; a taxonomy may define additive category union
and explicit domain-specific rules but never an undocumented downgrade. A derived
value inherits all contributing restrictions unless a reviewed transformation
proves removal/generalization and receives a declassification decision.

```text
classification.resolve|explain
classification.labels.propose|review|publish|reclassify
classification.policies.validate|publish|quarantine
classification.propagation.plan|record|status|reconcile
dlp.evaluate|explain
declassification.request|review|approve|revoke
```

Every consumer stores or can resolve the label/policy fingerprint and returns a
propagation receipt/watermark. Search, analytics, RAG, messaging, connectors,
models, telemetry, files, and exports reject data above their eligibility.

## Failure, Security, and Evidence

Missing, contradictory, expired, low-confidence-sensitive, or stale labels fail
closed for export/model/messaging/connector/file and lower-class-store paths.
Internal domain writes may quarantine the affected field/object rather than lose
the whole transaction when policy explicitly permits partial quarantine. A DLP
scanner outage queues/quarantines; it does not default allow.

Taxonomy author, schema/domain label proposer, security/privacy approver,
declassification reviewer, and DLP exception executor are separate authorities.
Downgrade requires purpose, transformation proof, reviewer, scope, expiry, and
consumer re-propagation. Emergency bypass cannot send unrestricted sensitive data
to an unapproved destination.

Evidence records inputs/labels/provenance, taxonomy/policy fingerprints,
combination trace, destination/recipient/purpose, DLP decision and obligations,
redaction/transformation hashes, declassification, propagation receipts,
unresolved consumers, exceptions, revocation, and incidents. Policy snapshots are
signed and locally applied.

Gate A implements pilot field/content labels, safe ingress, UI/repository masks,
telemetry redaction, and connector/export/model eligibility. Gate B applies the
same controls to writes, outbox, repair, and evidence exports.
