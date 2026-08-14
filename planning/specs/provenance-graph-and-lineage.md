# Provenance Graph and Lineage Contract

The Provenance Service owns a rebuildable, authorization-aware lineage projection.
Each source publisher owns the truth of the edges it publishes. The graph cannot
invent authority, business facts, or permission.

## Typed Graph Model

Node types include:

```text
SourceAssertion      Artifact             Transformation
Calculation          PolicyOrRule         Decision
BusinessIntent       ProposalRevision     WorkflowExecution
TransactionPlan      Transaction          LedgerEvent
ProjectionValue      ExternalEffect       ExternalObservation
Reconciliation       Repair               Filing/BillingRecord
Outcome
```

Edge types include:

```text
USED       DERIVED_FROM       GENERATED       CAUSED
DECIDED_BY GOVERNED_BY        EXECUTED_AS     PROJECTED_AS
SENT_AS    OBSERVED_AS        RECONCILED_WITH REPAIRED_BY
SUPERSEDES CORRECTS           CONTRIBUTED_TO  RESULTED_IN
```

`ProvenanceNode` and `ProvenanceEdge` carry stable IDs, publisher, source
record/event, schema/version, effective/recorded/published time, tenant/org/
subject scope, classification, purpose limits, confidence where derived,
correlation, retention, and publisher watermark.

## Lifecycle and APIs

```text
edge: PROPOSED -> VALIDATED -> ACTIVE -> SUPERSEDED | CORRECTED | RETIRED
publisher: REGISTERED -> ACTIVE -> LAGGING | INCOMPLETE | QUARANTINED
```

```text
provenance.publish|correct|supersede
provenance.traverse|explain_value|impact
provenance.completeness.verify|status
provenance.views.redact
provenance.rebuild|reconcile
provenance.publishers.register|quarantine
```

Publish is idempotent by publisher/source/edge identity and validates endpoint
existence or a declared pending reference. Each required workflow/domain/
connector/decision publisher reports watermarks and expected edge classes.
Queries return completeness status and source watermarks. Missing/lagging/
quarantined publishers produce `PARTIAL_LINEAGE`; the UI/agent/audit package may
not call it complete.

## Security, Consistency, and Evidence

Traversal reauthorizes every node and edge by tenant, org, subject, field/data
domain, purpose, relationship, classification, and inference risk. If an edge
would disclose a protected relationship, the view returns an opaque boundary or
redacted aggregate while preserving that lineage is incomplete to the caller.
Possession of one node ID never grants adjacent-node access.

The graph is derived and eventually consistent. Critical receipts store direct
source IDs independent of graph availability. A declared maximum lag applies to
customer explanations; stale graphs fail with visible partial status rather than
inventing edges. Rebuild replays authoritative ledgers/registries/journals and
compares counts/hashes/watermarks before alias switch.

Evidence records publisher registrations, schema and required-edge manifests,
publish/correction receipts, watermarks, completeness checks, redactions,
traversal request/purpose, sources consulted, partial reasons, rebuild and
reconciliation. Retention follows source edges and Records policy; deleting a
payload may retain a minimal non-identifying tombstone where authorized.

Gate A implements source -> mapping/transform -> normalized fact -> simulation ->
proposal lineage. Gate B adds approval -> plan -> transaction/events -> external
effect/observation -> reconciliation/repair lineage and completeness fixtures.
