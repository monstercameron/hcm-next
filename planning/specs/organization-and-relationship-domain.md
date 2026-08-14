# Organization and Relationship Domain Contract

The Organization Domain owns effective-dated organization units and business
relationships. Organization-scoped AuthZ consumes a versioned graph projection;
it does not own or repair the graph.

## Canonical State and Graphs

```text
OrganizationUnit
  DRAFT -> ACTIVE -> FROZEN? -> CLOSED

OrganizationRevision
  name/type/owner/legal-entity/cost-center/location over [from,to)

RelationshipEdge / RelationshipRevision
  source, target, type, direction, cardinality, effective interval

ManagerRelationship
ExternalOrgReference
ReorganizationPlan
GraphCorrection
```

## Workforce Relationship Authority Matrix

The Organization and Relationship Domain is the single canonical owner of typed
workforce relationship edges. People and Position may retain IDs and authorized
projections but cannot independently write the same relationship.

| Semantic relationship       | Canonical owner/stream                                | Consumer representation                                                                         |
| --------------------------- | ----------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `AssignmentReportsToPerson` | `ManagerRelationship` stream in this domain           | Assignment stores `manager_relationship_ref`; current manager is projected at a graph watermark |
| `PositionReportsToPosition` | Typed position relationship stream in this domain     | Position projection may expose `reports_to_position_ref`; PositionVersion does not own it       |
| `OrganizationUnitLeader`    | Typed organization relationship stream in this domain | Org/People views expose authorized projection                                                   |
| `TemporaryActingManager`    | Effective-dated relationship edge in this domain      | Resolver applies only for declared purposes/interval                                            |
| `DottedLineManager`         | Effective-dated relationship edge in this domain      | Never treated as primary manager unless the caller explicitly requests that type                |
| `ProjectLead`               | Project relationship edge in this domain              | Does not imply employment manager, approval authority, or general AuthZ                         |

`ManagerOf(subject, assignment, effective_at, purpose)` resolves from one verified
graph snapshot. Default precedence is an applicable temporary acting relationship,
then direct `AssignmentReportsToPerson`, then occupied
`PositionReportsToPosition`; `OrganizationUnitLeader` is fallback only when a
published resolver policy explicitly permits it. Dotted-line and project edges
are excluded unless named by the request.

Multiple assignments require an assignment ID or an explicit primary-assignment
rule. A vacant manager position returns `UNRESOLVED_MANAGER` plus fallback
obligation; it never guesses an org leader. The result includes relationship ID/
type, source, effective interval, graph watermark, resolver-policy version, and
confidence/ambiguity. Disagreement among projections blocks write revalidation,
routes reconciliation, and cannot be resolved by whichever cache is newest.

Multiple legitimate graphs coexist: corporate ownership, supervisory hierarchy,
cost-center hierarchy, legal-entity structure, geographic structure, project/
matrix teams, and temporary responsibility. A relationship type declares allowed
node types, direction, cardinality, cycle policy, legal-entity boundary rule,
inheritance meaning, and whether AuthZ may consume it.

## Commands, Queries, and Events

```text
organization.create|revise|reparent|merge|split|freeze|close|correct
relationships.create|revise|end|correct
organization.read|as_of|ancestry|descendency|impact|explain
relationships.query|as_of
relationships.manager_of|manager_chain
reorganizations.plan|simulate|approve|execute|reconcile
```

Events include `OrganizationUnitCreated/Revised/Reparented/Merged/Split/Closed`,
`RelationshipCreated/Revised/Ended`, `ManagerRelationshipChanged`,
`ReorganizationExecuted`, and `OrganizationGraphCorrected`.

Commands bind expected stream/graph version, effective range, SourceAuthority,
reference versions, normalized affected nodes/edges, AuthZ/legal decisions,
idempotency, and approval digest. Planned appends commit through the Multi-Stream
Transaction Contract. Workflow cannot write graph storage.

A Gate B manager change atomically commits the canonical relationship edge and
outbox with affected People/Position transaction streams where co-located. People,
approval-routing, AuthZ, and connector projections consume that event and expose
their applied watermark. Until required projections agree or bounded direct
canonical resolution is available, execution that depends on the manager fails
revalidation.

## Invariants, Failure, and Reconciliation

- Required trees have one permitted parent per effective instant; graph types
  that allow multiple parents state that explicitly.
- Cycle checks are relationship-type and effective-time specific.
- A unit cannot close while active/future assignments, positions, policies,
  budgets, queues, or role bindings lack migration/cancellation plans.
- Merge/split/reparent preserve former IDs/edges and historical as-of queries.
- Manager relationships reference active workers/assignments and obey maximum
  cardinality; dotted-line/project leadership is a distinct edge type.
- Legal-entity crossing edges cannot imply employment transfer or data access.
- Graph corrections append lineage and invalidate affected projections/caches/
  authorization decisions by watermark.

Typed failures include missing/inactive node, illegal edge/cardinality, cycle,
legal-boundary violation, stale graph, future conflict, dependent-resource block,
authority denial, and reconciliation ambiguity. External organization sources are
observations unless authority policy says otherwise; drift creates a plan/repair,
not automatic destructive reparenting.

## Security and Evidence

Read and change permissions evaluate tenant, graph type, source/target scope,
fields, effective time, purpose, current/proposed state, and risk. Relationship
visibility can itself be sensitive. Reorganization, merge/split, and graph
correction require separate capabilities, impact simulation, step-up, and dual
approval. AuthZ repositories use the last verified graph watermark and fail
closed for writes when required graph freshness is exceeded.

Evidence includes prior/proposed nodes/edges, graph/source versions, impact set,
cycle/cardinality/legal checks, approvals, transaction/events, invalidation
watermarks, external observations, reconciliation, and corrections.

Gate A implements authorized as-of reads and impact/cycle simulation. Gate B
implements only manager/organization relationship change required by Promotion;
unit create/reparent/merge/split/close and broad reorganization remain conformance.

Conformance covers assignment-direct, position-derived, acting, dotted-line,
project, vacant manager position, multiple assignments, projection disagreement,
external drift, and concurrent relationship change.
