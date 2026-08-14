# Records Management and Disposition Contract

The Records Management Service owns record declaration, series inventory, cutoff,
retention eligibility, archival transfer, disposition review, and certificates.
Legal/Regulatory supplies rules; domains declare records; Secure Deletion executes
approved destruction.

## Canonical State

```text
RecordSeries
RecordDeclaration
RetentionSchedule
CutoffEvent
HoldIntersection
DispositionPlan
DispositionReview
ArchiveTransfer
DispositionCertificate
VitalRecordDesignation
```

```text
Record: DECLARED -> ACTIVE -> CUTOFF -> ELIGIBLE
        -> ON_HOLD | ARCHIVE_PENDING | DISPOSITION_PENDING
        -> ARCHIVED | DESTROYED

DispositionPlan: DRAFT -> INVENTORIED -> HOLD_CHECKED -> APPROVED
                 -> EXECUTING -> VERIFIED -> CERTIFIED | REPAIR_REQUIRED
```

A record declaration identifies series/version, source business object/event,
authoritative and derived copies, tenant/org/legal entity, subject, classification,
custodian, vital-record status, trigger/cutoff facts, retention schedule/version,
legal context, hold keys, storage/key references, and expected disposition method.

## APIs and Rule Composition

```text
records.declare|read|inventory|correct
record_series.propose|publish|retire
retention.resolve|explain
cutoffs.record|correct
disposition.plan|review|approve|execute|verify|certify
archive.transfer|acknowledge|verify
holds.intersections.query
```

Minimum retention constraints combine by the longest applicable minimum;
mandatory maximum/destruction requirements remain visible as conflicts requiring
legal resolution rather than being silently overridden. Holds suspend only their
matching records/copies and record why. Trigger correction recalculates eligibility
and preserves the prior calculation. Derived copies cannot receive shorter
retention merely because they are rebuildable if they independently became a
record or evidence.

## Failure, Security, and Evidence

Unknown series, absent custodian, unresolved retention conflict, incomplete copy
inventory, uncertain cutoff, active hold, missing archive acknowledgement,
unverifiable deletion, or backup reappearance blocks certification. Indefinite
hold/retention is never the silent default; overdue unresolved items are tracked
obligations.

Declare, schedule-author, hold-administer, disposition-review, destroy, archive,
and certify are separate capabilities. Records access follows source classification
and purpose; disposition staff do not automatically gain payload access. Destruction
requires dual approval for sensitive/financial/legal series and delegates bytes
only through the Secure Deletion service.

Evidence includes source declaration, complete known-copy inventory and
watermarks, schedule/legal versions, cutoff and corrections, hold intersections,
eligibility calculation, review/approval, archive manifest/receipt or deletion
plan/results, backup re-delete state, exceptions/repair, and signed certificate.

## Tenant Isolation and Content Deduplication

Sensitive artifacts MUST NOT be deduplicated across tenants by a shared plaintext
content hash. An implementation may deduplicate within one tenant security domain,
or may share physical bytes only when all of the following are true:

```text
tenant-scoped encryption or key wrapper
+ tenant-scoped logical ownership record
+ transactional reference accounting
+ independent hold and retention evaluation
+ deletion that cannot disclose another tenant's existence
+ restore metadata that preserves the same ownership boundaries
```

Deleting tenant A MUST neither delete tenant B's surviving artifact nor keep
tenant A's data recoverable merely because tenant B references equal content.
Conformance tests cover delete-A/preserve-B, restore, re-delete, legal hold,
tenant exit, key revocation, hash-oracle resistance, and interrupted reference
updates. A shared object hash is addressing metadata, never proof that two tenants
may share retention, access, encryption, or deletion state.

Gate A declares proposal/simulation/decision/observation records and tests
retention resolution. Gate B adds approvals, transactions, integration journals,
repair/evidence artifacts, cutoffs, holds, and a non-destructive disposition
simulation. Actual pilot destruction follows the Gate C retention/deletion gate
unless contract or data-subject law requires earlier execution.
